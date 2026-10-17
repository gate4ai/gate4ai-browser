// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gate4ai/sdk-go/abi"
	"github.com/gate4ai/sdk-go/hosttest"
	"github.com/gate4ai/sdk-go/manifest"

	"github.com/gate4ai/gate4ai-browser/worker"
)

// fakeEngine — страница без Chrome: переходы, текст, поиск элемента для act.
type fakeEngine struct {
	url  string
	keys []string
}

func (f *fakeEngine) Navigate(_ context.Context, u string) error {
	if strings.HasSuffix(u, "/redirect") {
		u = "https://evil.example/"
	}
	f.url = u
	return nil
}
func (f *fakeEngine) URL(context.Context) (string, error) { return f.url, nil }
func (f *fakeEngine) Title(context.Context) (string, error) {
	return "Личный кабинет", nil
}
func (f *fakeEngine) Text(context.Context) (string, error) {
	return "Задолженность 1 234,56 ₽", nil
}
func (f *fakeEngine) Screenshot(_ context.Context, sel string) ([]byte, error) {
	return []byte("\x89PNG-fake"), nil
}
func (f *fakeEngine) Extract(context.Context, string) (any, error) {
	return map[string]any{"debt": 1234.56}, nil
}
func (f *fakeEngine) Eval(_ context.Context, expr string, out any) error {
	if strings.Contains(expr, "#missing") {
		return json.Unmarshal([]byte("null"), out)
	}
	if strings.Contains(expr, "#away") {
		f.url = "https://evil.example/"
	}
	return json.Unmarshal([]byte(`{"text":"ok"}`), out)
}
func (f *fakeEngine) InsertText(_ context.Context, t string) error {
	f.keys = append(f.keys, t)
	return nil
}
func (f *fakeEngine) Key(_ context.Context, k string) error { f.keys = append(f.keys, k); return nil }
func (f *fakeEngine) Close() error                          { return nil }

func setup(t *testing.T, settings map[string]any) (*hosttest.Host, *Browser, *fakeEngine) {
	t.Helper()
	m, err := manifest.Parse(manifestYAML)
	if err != nil {
		t.Fatal(err)
	}
	eng := &fakeEngine{}
	b := &Browser{W: &worker.Service{NewEngine: func(context.Context, worker.Settings, string) (worker.Engine, error) { return eng, nil }}}
	if settings == nil {
		settings = map[string]any{"allowed_domains": []any{"*.example.com", "app.example.org"}, "cdp_url": "http://127.0.0.1:1"}
	}
	return hosttest.New(m, settings, nil), b, eng
}

func TestAgentReadScreenshotExtract(t *testing.T) {
	h, b, _ := setup(t, nil)
	res, err := h.Execute(b, "read", map[string]any{"url": "https://app.example.org/"})
	if err != nil {
		t.Fatal(err)
	}
	p := res.Structured.(map[string]any)
	if p["title"] != "Личный кабинет" || !strings.Contains(res.Content[0].Text, "1 234,56") || res.Source != "web:https://app.example.org/" {
		t.Fatalf("%+v", res)
	}
	res, err = h.Execute(b, "screenshot", map[string]any{"url": "https://app.example.org/", "selector": "#debt"})
	if err != nil || string(res.Evidence[0].Data) != "\x89PNG-fake" || res.Content[0].Type != abi.ContentImage {
		t.Fatal(res, err)
	}
	res, err = h.Execute(b, "extract", map[string]any{"url": "https://app.example.org/", "instruction": "debt"})
	if err != nil || res.Structured.(map[string]any)["data"].(map[string]any)["debt"] != 1234.56 {
		t.Fatal(res, err)
	}
}

func TestAgentSites(t *testing.T) {
	h, b, _ := setup(t, nil)
	for _, u := range []string{"https://evil.example/", "https://example.com.evil.example/", "https://app.example.org/redirect"} {
		if _, err := h.Execute(b, "read", map[string]any{"url": u}); err == nil || !strings.Contains(err.Error(), abi.CodeCapabilityDenied) {
			t.Errorf("%s: %v", u, err)
		}
	}
	h.Settings = map[string]any{"cdp_url": "http://127.0.0.1:1"}
	if _, err := h.Execute(b, "read", map[string]any{"url": "https://x.example"}); err == nil || !strings.Contains(err.Error(), "allowed_domains is empty") {
		t.Fatal("empty allowlist must deny:", err)
	}
	h.Settings = map[string]any{"mode": "blocklist", "blocked": []any{"*.evil.example"}, "cdp_url": "http://127.0.0.1:1"}
	if _, err := h.Execute(b, "read", map[string]any{"url": "https://shop.example/"}); err != nil {
		t.Fatal("blocklist allows other sites:", err)
	}
	for _, u := range []string{"https://a.evil.example/", "http://127.0.0.1:8080/"} {
		if _, err := h.Execute(b, "read", map[string]any{"url": u}); err == nil {
			t.Errorf("blocklist allowed %s", u)
		}
	}
}

func exec(b *Browser, action string, args map[string]any, caller *abi.Caller) (abi.ExecResult, error) {
	return b.Execute(context.Background(), nil, abi.ExecRequest{Action: action, Args: args, Caller: caller,
		Settings: map[string]any{"cdp_url": "http://127.0.0.1:1", "allowed_domains": []any{"*.example.com"}}})
}

func TestServiceActions(t *testing.T) {
	_, b, eng := setup(t, nil)
	caller := &abi.Caller{Connector: "tbank", Call: "root1", Params: map[string]any{"domains": []any{"www.tbank-online.com"}}}
	if _, err := exec(b, "open", map[string]any{"url": "https://www.tbank-online.com/"}, nil); err == nil || !strings.Contains(err.Error(), "only for other connectors") {
		t.Fatal("service action without caller:", err)
	}
	if _, err := exec(b, "act", map[string]any{"selector": "#sum", "method": "fill", "value": "10"}, caller); err == nil || !strings.Contains(err.Error(), "open first") {
		t.Fatal("act before open:", err)
	}
	if _, err := exec(b, "open", map[string]any{"url": "https://evil.example/"}, caller); err == nil || !strings.Contains(err.Error(), abi.CodeCapabilityDenied) {
		t.Fatal("site outside caller params:", err)
	}
	res, err := exec(b, "open", map[string]any{"url": "https://www.tbank-online.com/"}, caller)
	if err != nil || res.Structured.(map[string]any)["url"] != "https://www.tbank-online.com/" {
		t.Fatal(res, err)
	}
	res, err = exec(b, "act", map[string]any{"selector": "#sum", "method": "fill", "value": "10"}, caller)
	if err != nil || res.Structured.(map[string]any)["text"] != "ok" || eng.keys[len(eng.keys)-1] != "10" {
		t.Fatal(res, err)
	}
	for _, bad := range []map[string]any{{"selector": "#x", "method": "hover"}, {"selector": "#x", "method": "press", "value": "F5"}, {"method": "click"}} {
		if _, err := exec(b, "act", bad, caller); err == nil || !strings.Contains(err.Error(), abi.CodeBadRequest) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	if _, err := exec(b, "act", map[string]any{"selector": "#missing", "method": "click", "timeout_ms": 1.0}, caller); err == nil || !strings.Contains(err.Error(), abi.CodeNotFound) {
		t.Fatal("missing element:", err)
	}
	if _, err := exec(b, "act", map[string]any{"selector": "#away", "method": "click"}, caller); err == nil || !strings.Contains(err.Error(), abi.CodeCapabilityDenied) {
		t.Fatal("click led off the site list:", err)
	}
	res, err = exec(b, "observe", map[string]any{}, caller)
	if err == nil {
		// после ухода со списка страница закрыта (about:blank)
		t.Fatal("observe after leaving the list:", res)
	}
	// Страница — одна на корневое действие: другой корень ждёт end_call.
	done := make(chan error, 1)
	go func() {
		_, err := exec(b, "open", map[string]any{"url": "https://www.tbank-online.com/"}, &abi.Caller{Connector: "tbank", Call: "root2", Params: caller.Params})
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("second page while the first is held")
	case <-time.After(100 * time.Millisecond):
	}
	b.EndCall("root1")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	b.EndCall("root2")
	if _, err := exec(b, "open", map[string]any{"url": "https://www.tbank-online.com/"}, &abi.Caller{Connector: "x", Call: "r3"}); err == nil || !strings.Contains(err.Error(), "allows no sites") {
		t.Fatal("caller without sites:", err)
	}
}

func TestIdleSessionReleased(t *testing.T) {
	old := SessionIdle
	SessionIdle = 50 * time.Millisecond
	defer func() { SessionIdle = old }()
	_, b, _ := setup(t, nil)
	caller := &abi.Caller{Connector: "tbank", Call: "lost", Params: map[string]any{"domains": []any{"*.example.com"}}}
	if _, err := exec(b, "open", map[string]any{"url": "https://a.example.com/"}, caller); err != nil {
		t.Fatal(err)
	}
	// end_call не пришёл — страница освобождается по простою.
	if _, err := exec(b, "read", map[string]any{"url": "https://a.example.com/"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestManifest(t *testing.T) {
	m, err := manifest.Parse(manifestYAML)
	if err != nil {
		t.Fatal(err)
	}
	if m.Runtime != manifest.RuntimeNative || m.Contract != 1 || len(m.Platforms) < 4 {
		t.Fatal(m.Runtime, m.Contract, m.Platforms)
	}
	for _, a := range m.Actions {
		if a.ServiceOnly != m.IsService(a.Name) {
			t.Errorf("%s: service_only must match services", a.Name)
		}
	}
	if len(m.Capabilities.Network) != 0 || len(m.Capabilities.Files) != 0 || len(m.Capabilities.Reveal) != 1 {
		t.Fatal("browser declares only reveal of the model key", m.Capabilities)
	}
}
