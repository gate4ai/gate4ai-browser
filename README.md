# gate4ai-browser — коннектор «Браузер» для gate4.ai

Браузерный воркер в выделенном профиле настоящего Chrome (CDP, Stagehand) — нативный пакет (`runtime: native`): отдельный процесс под надзором хоста [gate4ai](https://github.com/gate4ai/gate4ai). Работает без песочницы, с правами пользователя, поэтому магазин принимает нативные пакеты только от gate4.ai.

- **Агенту:** `read` (текст страницы), `screenshot` (снимок элемента — доказательство), `extract` (данные по инструкции, модель задаёт владелец) на сайтах из настроек: `allowed_domains` или режим `blocklist` с `blocked` (адреса этого компьютера и локальной сети закрыты всегда). Итоговый адрес проверяется и после редиректов.
- **Другим коннекторам** (`service_call`, контракт 1): `open`, `observe`, `act` (click / fill / press / wait по CSS-селектору и видимому тексту), `page_extract`, `page_screenshot`. Страница — одна на корневое действие агента, сайты — `params` вызывающего (`{mode, domains, blocked}`), после каждого действия адрес проверяется снова. Клиент — `gate4ai contract gen manifest.yaml`.

## Установка

```sh
gate4ai connectors install browser                 # из магазина gate4.ai (ставится и сам при первом запуске)
# или своя сборка под этот компьютер:
gate4ai connectors install . --dev
```

Настройки — `connectors.browser` в конфигурации gate4ai: `allowed_domains`, `mode`, `blocked`, `engine` (stagehand | cdp), `cdp_url`, `chrome_path`, `profile_dir`, `model`, `model_key` (алиас Vault: значение хост передаёт пакету по `capabilities.reveal`), `max_text_chars`.

## Разработка

```sh
go test ./...                                     # коннектор — на sdk-go/hosttest, воркер — на фейковом CDP
gate4ai connectors pack .                         # пакет под каждую платформу из manifest.platforms + browser-<версия>.platforms.json
gate4ai connectors install . --watch              # поставить в свой daemon из исходников и пересобирать на каждое сохранение
```

Несовместимое изменение действий из `services` — только с повышением `contract` в манифесте (CI проверяет `gate4ai contract diff` с прошлым тегом).

## Лицензия

[AGPL-3.0-only](LICENSE). Хост gate4ai тоже под AGPL-3.0. Вклад — по CLA (подписывается в первом pull request).
