// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/gate4ai/sdk-go/abi"
)

// Browser — коннектор.
type Browser struct{}

// Execute исполняет действие.
func (Browser) Execute(ctx context.Context, h *abi.Host, r abi.ExecRequest) (abi.ExecResult, error) {
	if l, _ := r.Settings["allowed_domains"].([]any); len(l) == 0 {
		return abi.ExecResult{}, errors.New("connectors.browser.allowed_domains is empty: no sites are allowed")
	}
	u, _ := r.Args["url"].(string)
	page, err := h.BrowserOpen(u)
	if err != nil {
		return abi.ExecResult{}, err
	}
	switch r.Action {
	case "read":
		p, err := h.BrowserObserve(0)
		if err != nil {
			return abi.ExecResult{}, err
		}
		res := map[string]any{"url": p.URL, "title": p.Title}
		if p.Truncated {
			res["truncated"] = true
		}
		return abi.ExecResult{Structured: res, Content: []abi.Content{abi.Text(p.Text)}, ResultRef: p.URL, Source: "web:" + p.URL}, nil
	case "screenshot":
		sel, _ := r.Args["selector"].(string)
		png, err := h.BrowserScreenshot(sel)
		if err != nil {
			return abi.ExecResult{}, err
		}
		return abi.ExecResult{Structured: map[string]any{"url": page.URL, "selector": sel}, Content: []abi.Content{abi.Image("image/png", png)},
			ResultRef: page.URL, Evidence: []abi.EvidenceItem{{Name: "element.png", MIME: "image/png", Data: png}}}, nil
	case "extract":
		ins, _ := r.Args["instruction"].(string)
		data, err := h.BrowserExtract(ins)
		if err != nil {
			return abi.ExecResult{}, err
		}
		return abi.ExecResult{Structured: map[string]any{"url": page.URL, "data": data}, ResultRef: page.URL, Source: "web:" + page.URL}, nil
	}
	return abi.ExecResult{}, fmt.Errorf("unknown action %s", r.Action)
}

// Verify — у действий чтения проверки нет (хост её не вызывает).
func (Browser) Verify(_ context.Context, _ *abi.Host, r abi.VerifyRequest) (abi.VerifyResult, error) {
	return abi.VerifyResult{}, abi.Errorf(abi.CodeBadRequest, "action %s has no verification", r.Action)
}
