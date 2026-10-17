// SPDX-License-Identifier: AGPL-3.0-only

package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gate4ai/sdk-go/domain"
)

// Settings — настройки браузера (connectors.browser в конфигурации gate4ai).
type Settings struct {
	Engine       string // stagehand (по умолчанию) | cdp
	CDPURL       string // уже запущенный Chrome; иначе воркер запускает его сам
	ChromePath   string
	ProfileDir   string // выделенный профиль (по умолчанию ~/.gate4ai-chrome)
	Model        string // модель для extract, например "openai/gpt-5.4-mini"
	ModelKey     string // значение ключа модели (хост передаёт его по capabilities.reveal)
	MaxTextChars int
}

// EngineFactory создаёт движок (подменяется в тестах).
type EngineFactory func(ctx context.Context, s Settings, cdpURL string) (Engine, error)

// DefaultEngine — Stagehand или прямой CDP.
func DefaultEngine(ctx context.Context, s Settings, cdpURL string) (Engine, error) {
	if s.Engine == "cdp" {
		return DialCDP(ctx, cdpURL)
	}
	return DialStagehand(ctx, StagehandConfig{CDPURL: cdpURL, ModelName: s.Model, APIKey: s.ModelKey})
}

// Service — браузерный воркер: один движок между вызовами, одна сессия за
// раз — страница не делится между вызовами.
type Service struct {
	NewEngine EngineFactory
	Launch    func(ctx context.Context, chrome, profile string) (string, *os.Process, error)

	session sync.Mutex // одна сессия за раз
	mu      sync.Mutex
	exec    *Executor
	key     string
	proc    *os.Process
}

// New — воркер с движками по умолчанию.
func New() *Service {
	return &Service{NewEngine: DefaultEngine, Launch: Launch}
}

// normalize — значения по умолчанию.
func normalize(st Settings) Settings {
	if st.MaxTextChars <= 0 {
		st.MaxTextChars = 30000
	}
	if st.ProfileDir == "" {
		home, _ := os.UserHomeDir()
		st.ProfileDir = filepath.Join(home, ".gate4ai-chrome")
	}
	return st
}

func (s *Service) executor(ctx context.Context, st Settings) (*Executor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprint(st.CDPURL, st.ChromePath, st.ProfileDir, st.Engine, st.Model, st.ModelKey)
	if s.exec != nil && s.key == key {
		return s.exec, nil
	}
	if s.exec != nil {
		_ = s.exec.Engine.Close()
		s.exec = nil
	}
	cdp := st.CDPURL
	if cdp == "" {
		chrome := st.ChromePath
		if chrome == "" {
			var err error
			if chrome, err = FindChrome(); err != nil {
				return nil, err
			}
		}
		u, proc, err := s.Launch(ctx, chrome, st.ProfileDir)
		if err != nil {
			return nil, err
		}
		cdp, s.proc = u, proc
	}
	eng, err := s.NewEngine(ctx, st, cdp)
	if err != nil {
		return nil, err
	}
	s.exec = &Executor{Engine: eng, Pacing: NoPacing}
	s.key = key
	return s.exec, nil
}

// Acquire — сессия (одна за раз) с настройками st; Release освобождает её.
func (s *Service) Acquire(ctx context.Context, st Settings) (*Session, error) {
	s.session.Lock()
	st = normalize(st)
	x, err := s.executor(ctx, st)
	if err != nil {
		s.session.Unlock()
		return nil, err
	}
	return &Session{s: s, x: x, max: st.MaxTextChars}, nil
}

// Close закрывает движок.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exec != nil {
		_ = s.exec.Engine.Close()
		s.exec = nil
	}
	return nil
}

// Session — страница одного вызова.
type Session struct {
	s    *Service
	x    *Executor
	max  int
	done bool
}

// Open переходит по URL; sites — разрешённые сайты, итоговый адрес после
// редиректов тоже проверяется.
func (ss *Session) Open(ctx context.Context, url string, sites domain.Sites) (Page, error) {
	ss.x.Sites = sites
	final, err := ss.x.Open(ctx, url)
	if err != nil {
		return Page{}, err
	}
	title, _ := ss.x.Engine.Title(ctx)
	return Page{URL: final, Title: title}, nil
}

// Observe — адрес, заголовок и текст страницы (до max символов).
func (ss *Session) Observe(ctx context.Context, max int) (Page, error) {
	if max <= 0 || max > ss.max {
		max = ss.max
	}
	u, title, text, trunc, err := ss.x.Observe(ctx, max)
	return Page{URL: u, Title: title, Text: text, Truncated: trunc}, err
}

// Extract — данные со страницы по инструкции.
func (ss *Session) Extract(ctx context.Context, instruction string) (any, error) {
	return ss.x.Extract(ctx, instruction)
}

// ScreenshotElement — снимок элемента по CSS-селектору.
func (ss *Session) ScreenshotElement(ctx context.Context, selector string) ([]byte, error) {
	return ss.x.Screenshot(ctx, selector)
}

// Act — click / fill / press / wait по селектору; элемента нет — ErrNotFound.
func (ss *Session) Act(ctx context.Context, r ActRequest) (ActResponse, error) {
	return ss.x.Act(ctx, r)
}

// Release освобождает сессию.
func (ss *Session) Release() {
	if ss.done {
		return
	}
	ss.done = true
	ss.x.Sites = domain.Sites{}
	ss.s.session.Unlock()
}
