// SPDX-License-Identifier: AGPL-3.0-only

// Коннектор gate4.ai «Браузер» — нативный пакет (runtime: native): браузерный
// воркер (выделенный профиль Chrome, CDP, Stagehand) в отдельном процессе под
// надзором gate4ai. Агенту — чтение, скриншот и извлечение на сайтах из
// настроек (allowed_domains / blocked); другим коннекторам — действия с
// открытой страницей через service_call (open, observe, act, page_extract,
// page_screenshot) на сайтах из их params.sites. Ключ модели для extract хост
// передаёт по capabilities.reveal.
//
// Лицензия: AGPL-3.0-only.
package main

import (
	_ "embed"

	"github.com/gate4ai/sdk-go/native"
)

//go:embed manifest.yaml
var manifestYAML []byte

func main() { native.Serve(manifestYAML, New()) }
