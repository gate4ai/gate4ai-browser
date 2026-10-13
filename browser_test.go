// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/gate4ai/sdk-go/abi"
	"github.com/gate4ai/sdk-go/connector"
	"github.com/gate4ai/sdk-go/hostfuncs"
	"github.com/gate4ai/sdk-go/hosttest"
)

// fakeBrowser — браузер хоста в тесте: страницы в памяти, один редирект.
type fakeBrowser struct{ url string }

func (b *fakeBrowser) Acquire(context.Context) (hostfuncs.BrowserSession, error) { return b, nil }
func (b *fakeBrowser) Open(_ context.Context, u string, _ []string) (abi.BrowserPage, error) {
	if strings.HasSuffix(u, "/redirect") {
		u = "https://evil.example/phish"
	}
	b.url = u
	return abi.BrowserPage{URL: u, Title: "Личный кабинет"}, nil
}
func (b *fakeBrowser) Observe(context.Context, int) (abi.BrowserPage, error) {
	return abi.BrowserPage{URL: b.url, Title: "Личный кабинет", Text: "Задолженность: 1 234,56 ₽"}, nil
}
func (b *fakeBrowser) Extract(_ context.Context, ins string) (any, error) {
	return map[string]any{"debt": 1234.56}, nil
}
func (b *fakeBrowser) ScreenshotElement(context.Context, string) ([]byte, error) {
	return []byte("\x89PNG-fake"), nil
}
func (b *fakeBrowser) Release() {}

func setup(t *testing.T) *hosttest.Host {
	m, _, err := connector.Registered()
	if err != nil {
		t.Fatal(err)
	}
	h := hosttest.New(m, map[string]any{"allowed_domains": []any{"*.gosuslugi.ru", "lk.mosenergosbyt.ru"}}, nil)
	h.Browser = &fakeBrowser{}
	return h
}

func TestReadScreenshotExtract(t *testing.T) {
	h := setup(t)
	c := Browser{}
	res, err := h.Execute(c, "read", map[string]any{"url": "https://lk.mosenergosbyt.ru/"})
	if err != nil {
		t.Fatal(err)
	}
	p := res.Result.(map[string]any)
	if p["title"] != "Личный кабинет" || !strings.Contains(p["text"].(string), "1 234,56") || res.Source != "web:https://lk.mosenergosbyt.ru/" {
		t.Fatalf("%+v", res)
	}
	res, err = h.Execute(c, "screenshot", map[string]any{"url": "https://lk.mosenergosbyt.ru/", "selector": "#debt"})
	if err != nil || string(res.Evidence[0].Data) != "\x89PNG-fake" {
		t.Fatal(res, err)
	}
	res, err = h.Execute(c, "extract", map[string]any{"url": "https://lk.mosenergosbyt.ru/", "instruction": "debt"})
	if err != nil || res.Result.(map[string]any)["data"].(map[string]any)["debt"] != 1234.56 {
		t.Fatal(res, err)
	}
}

func TestDomainAllowlist(t *testing.T) {
	h := setup(t)
	for _, u := range []string{"https://evil.example/", "https://gosuslugi.ru.evil.example/", "https://lk.mosenergosbyt.ru/redirect"} {
		if _, err := h.Execute(Browser{}, "read", map[string]any{"url": u}); err == nil || !strings.Contains(err.Error(), abi.CodeCapabilityDenied) {
			t.Errorf("%s: %v", u, err)
		}
	}
	h.Settings = map[string]any{}
	if _, err := h.Execute(Browser{}, "read", map[string]any{"url": "https://x.ru"}); err == nil {
		t.Fatal("empty allowlist must deny")
	}
}
