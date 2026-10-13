// SPDX-License-Identifier: AGPL-3.0-only

// Коннектор gate4.ai «Браузер» (чтение): тонкий модуль поверх функций хоста
// browser_*. Сам браузер (выделенный профиль Chrome, CDP, Stagehand) — служба
// хоста; домены — capabilities.browser.domains из настройки allowed_domains,
// хост проверяет их до и после редиректов.
//
// Лицензия: AGPL-3.0-only с дополнительным разрешением для хоста gate4ai
// (LICENSE-EXCEPTION).
package main

import (
	_ "embed"

	"github.com/gate4ai/sdk-go/connector"
)

//go:embed manifest.yaml
var manifestYAML []byte

func init() { connector.Register(manifestYAML, Browser{}) }

func main() {}
