# gate4ai-browser — коннектор «Браузер (чтение)» для gate4.ai

Чтение страниц, снимок элемента как доказательство и извлечение данных по инструкции в выделенном профиле Chrome. Сам браузер — служба хоста; сайты ограничены настройкой `allowed_domains`, хост проверяет их до и после редиректов.

Коннектор — WASM-модуль для хоста [gate4ai](https://github.com/gate4ai/gate4ai): что ему можно, объявлено в [manifest.yaml](manifest.yaml) (`capabilities`) и показывается владельцу при установке; всё остальное хост запрещает (`capability_denied`). Написан на [sdk-go](https://github.com/gate4ai/sdk-go).

## Установка

```sh
gate4ai connectors install browser                 # из магазина gate4.ai
# или своя сборка:
gate4ai connectors pack . -o browser.g4c && gate4ai connectors install browser.g4c --dev
```

Настройки — `connectors.browser` в конфигурации gate4ai (секреты — только алиасы Vault, `@имя`).

## Разработка

```sh
go test ./...                                                    # нативно, на тестовом хосте sdk-go/hosttest
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o connector.wasm .
```

## Лицензия

[AGPL-3.0-only](LICENSE) с [дополнительным разрешением](LICENSE-EXCEPTION) для хоста gate4ai: коннектор можно исполнять в хосте и распространять вместе с ним, не распространяя AGPL на хост. Изменения самого коннектора остаются под AGPL. Вклад — по DCO (`git commit -s`).
