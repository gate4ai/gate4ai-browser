// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gate4ai/sdk-go/abi"
	"github.com/gate4ai/sdk-go/domain"

	"github.com/gate4ai/gate4ai-browser/worker"
)

// SessionIdle — страница вызывающего коннектора освобождается, если end_call
// не пришёл (хост упал): иначе воркер ждал бы её вечно.
var SessionIdle = 2 * time.Minute

// Browser — коннектор: действия агента (read, screenshot, extract — страница
// на время действия, сайты — из настроек) и действия для других коннекторов
// через service_call (страница на корневое действие, сайты — params.sites
// вызывающего).
type Browser struct {
	W *worker.Service

	mu       sync.Mutex
	sessions map[string]*callSession // корневое действие → страница
	create   sync.Mutex              // создание страниц по очереди (воркер — одна сессия за раз)
}

type callSession struct {
	s      *worker.Session
	sites  domain.Sites
	opened bool
	timer  *time.Timer
	mu     sync.Mutex // действия одной страницы — по очереди
}

// New — коннектор с воркером по умолчанию.
func New() *Browser { return &Browser{W: worker.New()} }

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func strs(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		var out []string
		for _, x := range t {
			out = append(out, strs(x)...)
		}
		return out
	}
	return nil
}

// settings — настройки воркера из настроек коннектора и значения ключа модели (reveal).
func settings(r abi.ExecRequest) worker.Settings {
	s := r.Settings
	st := worker.Settings{Engine: str(s, "engine"), CDPURL: str(s, "cdp_url"), ChromePath: str(s, "chrome_path"),
		ProfileDir: str(s, "profile_dir"), Model: str(s, "model")}
	if alias := str(s, "model_key"); alias != "" {
		st.ModelKey = r.Revealed[alias]
	}
	if n, ok := s["max_text_chars"].(float64); ok {
		st.MaxTextChars = int(n)
	}
	return st
}

// sitesOf — список сайтов: mode allowlist | blocklist ("" — blocklist, если
// задан blocked, иначе allowlist), domains — разрешённые, blocked — запрещённые.
func sitesOf(m map[string]any, domainsKey string) (domain.Sites, error) {
	mode := str(m, "mode")
	blocked := strs(m["blocked"])
	switch mode {
	case "allowlist":
	case "blocklist":
		return domain.Sites{Blocklist: true, Domains: blocked}, nil
	case "":
		if len(blocked) > 0 {
			return domain.Sites{Blocklist: true, Domains: blocked}, nil
		}
	default:
		return domain.Sites{}, fmt.Errorf("mode %q must be allowlist or blocklist", mode)
	}
	return domain.Sites{Domains: strs(m[domainsKey])}, nil
}

// fail — ошибка воркера в коде ABI.
func fail(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, worker.ErrDomain):
		return abi.Errorf(abi.CodeCapabilityDenied, "%v", err)
	case errors.Is(err, worker.ErrNotFound):
		return abi.Errorf(abi.CodeNotFound, "%v", err)
	}
	var ae *abi.Error
	if errors.As(err, &ae) {
		return err
	}
	return abi.Errorf(abi.CodeIO, "%v", err)
}

// Execute исполняет действие.
func (b *Browser) Execute(ctx context.Context, h *abi.Host, r abi.ExecRequest) (abi.ExecResult, error) {
	switch r.Action {
	case "read", "screenshot", "extract":
		res, err := b.agent(ctx, r)
		return res, fail(err)
	}
	if r.Caller == nil {
		return abi.ExecResult{}, abi.Errorf(abi.CodeBadRequest, "action %s is only for other connectors (service_call)", r.Action)
	}
	res, err := b.service(ctx, r)
	return res, fail(err)
}

// agent — действие агента: страница на время действия.
func (b *Browser) agent(ctx context.Context, r abi.ExecRequest) (abi.ExecResult, error) {
	sites, err := sitesOf(r.Settings, "allowed_domains")
	if err != nil {
		return abi.ExecResult{}, fmt.Errorf("connectors.browser: %w", err)
	}
	if !sites.Blocklist && len(sites.Domains) == 0 {
		return abi.ExecResult{}, errors.New("connectors.browser.allowed_domains is empty: no sites are allowed")
	}
	s, err := b.W.Acquire(ctx, settings(r))
	if err != nil {
		return abi.ExecResult{}, err
	}
	defer s.Release()
	page, err := s.Open(ctx, str(r.Args, "url"), sites)
	if err != nil {
		return abi.ExecResult{}, err
	}
	switch r.Action {
	case "read":
		p, err := s.Observe(ctx, 0)
		if err != nil {
			return abi.ExecResult{}, err
		}
		res := map[string]any{"url": p.URL, "title": p.Title}
		if p.Truncated {
			res["truncated"] = true
		}
		return abi.ExecResult{Structured: res, Content: []abi.Content{abi.Text(p.Text)}, ResultRef: p.URL, Source: "web:" + p.URL}, nil
	case "screenshot":
		sel := str(r.Args, "selector")
		png, err := s.ScreenshotElement(ctx, sel)
		if err != nil {
			return abi.ExecResult{}, err
		}
		return abi.ExecResult{Structured: map[string]any{"url": page.URL, "selector": sel}, Content: []abi.Content{abi.Image("image/png", png)},
			ResultRef: page.URL, Evidence: []abi.EvidenceItem{{Name: "element.png", MIME: "image/png", Data: png}}}, nil
	}
	data, err := s.Extract(ctx, str(r.Args, "instruction"))
	if err != nil {
		return abi.ExecResult{}, err
	}
	return abi.ExecResult{Structured: map[string]any{"url": page.URL, "data": data}, ResultRef: page.URL, Source: "web:" + page.URL}, nil
}

// session — страница корневого действия вызывающего (создаётся при первом обращении).
func (b *Browser) session(ctx context.Context, r abi.ExecRequest) (*callSession, error) {
	b.create.Lock()
	defer b.create.Unlock()
	b.mu.Lock()
	if cs, ok := b.sessions[r.Caller.Call]; ok {
		cs.timer.Reset(SessionIdle)
		b.mu.Unlock()
		return cs, nil
	}
	b.mu.Unlock()
	sites, err := sitesOf(r.Caller.Params, "domains")
	if err != nil {
		return nil, fmt.Errorf("params of %s: %w", r.Caller.Connector, err)
	}
	if !sites.Blocklist && len(sites.Domains) == 0 {
		return nil, abi.Errorf(abi.CodeCapabilityDenied, "%s allows no sites (capabilities.services params.domains is empty)", r.Caller.Connector)
	}
	s, err := b.W.Acquire(ctx, settings(r))
	if err != nil {
		return nil, err
	}
	cs := &callSession{s: s, sites: sites}
	call := r.Caller.Call
	cs.timer = time.AfterFunc(SessionIdle, func() { b.EndCall(call) })
	b.mu.Lock()
	if b.sessions == nil {
		b.sessions = map[string]*callSession{}
	}
	b.sessions[call] = cs
	b.mu.Unlock()
	return cs, nil
}

// service — действие для другого коннектора на странице его корневого действия.
func (b *Browser) service(ctx context.Context, r abi.ExecRequest) (abi.ExecResult, error) {
	if r.Action == "act" {
		if err := worker.CheckAct(actRequest(r.Args)); err != nil {
			return abi.ExecResult{}, abi.Errorf(abi.CodeBadRequest, "%v", err)
		}
	}
	cs, err := b.session(ctx, r)
	if err != nil {
		return abi.ExecResult{}, err
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if r.Action != "open" && !cs.opened {
		return abi.ExecResult{}, abi.Errorf(abi.CodeBadRequest, "call browser.open first")
	}
	switch r.Action {
	case "open":
		p, err := cs.s.Open(ctx, str(r.Args, "url"), cs.sites)
		if err != nil {
			return abi.ExecResult{}, err
		}
		cs.opened = true
		return abi.ExecResult{Structured: map[string]any{"url": p.URL, "title": p.Title}, ResultRef: p.URL}, nil
	case "observe":
		max, _ := r.Args["max_chars"].(float64)
		p, err := cs.s.Observe(ctx, int(max))
		if err != nil {
			return abi.ExecResult{}, err
		}
		res := map[string]any{"url": p.URL, "title": p.Title, "text": p.Text}
		if p.Truncated {
			res["truncated"] = true
		}
		return abi.ExecResult{Structured: res, ResultRef: p.URL, Source: "web:" + p.URL}, nil
	case "act":
		res, err := cs.s.Act(ctx, actRequest(r.Args))
		if err != nil {
			return abi.ExecResult{}, err
		}
		return abi.ExecResult{Structured: map[string]any{"url": res.URL, "text": res.Text}, ResultRef: res.URL}, nil
	case "page_extract":
		data, err := cs.s.Extract(ctx, str(r.Args, "instruction"))
		if err != nil {
			return abi.ExecResult{}, err
		}
		return abi.ExecResult{Structured: map[string]any{"data": data}}, nil
	case "page_screenshot":
		png, err := cs.s.ScreenshotElement(ctx, str(r.Args, "selector"))
		if err != nil {
			return abi.ExecResult{}, err
		}
		return abi.ExecResult{Content: []abi.Content{abi.Image("image/png", png)}}, nil
	}
	return abi.ExecResult{}, fmt.Errorf("unknown action %s", r.Action)
}

func actRequest(a map[string]any) worker.ActRequest {
	t, _ := a["timeout_ms"].(float64)
	return worker.ActRequest{Selector: str(a, "selector"), Method: strings.ToLower(str(a, "method")), Text: str(a, "text"),
		Value: str(a, "value"), TimeoutMS: int(t)}
}

// EndCall освобождает страницу корневого действия (native.CallEnder).
func (b *Browser) EndCall(call string) {
	b.mu.Lock()
	cs, ok := b.sessions[call]
	delete(b.sessions, call)
	b.mu.Unlock()
	if !ok {
		return
	}
	cs.timer.Stop()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.s.Release()
}

// Verify — у действий браузера проверки нет (хост её не вызывает).
func (b *Browser) Verify(_ context.Context, _ *abi.Host, r abi.VerifyRequest) (abi.VerifyResult, error) {
	return abi.VerifyResult{}, abi.Errorf(abi.CodeBadRequest, "action %s has no verification", r.Action)
}
