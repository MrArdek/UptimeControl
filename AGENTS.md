# UptimeControl — инструкции для агента

Self-hosted мониторинг проектов: Go 1.27 (только stdlib `net/http`) + PostgreSQL 14–18. Зависимости: только `pgx/v5` и `x/crypto`.

## Документация — источник правды в `docs/`

`README.md` — это вижн продукта, а не факт. Сначала читать `docs/`. При конфликте docs vs код — верить коду.

| Файл | Что в нём |
|---|---|
| `PROJECT_STATE.md` | Фактическое состояние: что готово, что в работе, известные проблемы |
| `SETUP.md` | Запуск, переменные окружения, curl-примеры auth / projects / heartbeat |
| `VERIFICATION.md` | Команды проверки и результат последнего прогона |
| `API.md` | Актуальные маршруты (projects, monitors, heartbeat); `sites` — legacy |
| `DATABASE.md` | Таблицы, связи, правила изменения схемы |
| `DATA_CONTRACTS.md` | Определения метрик, ingest-контракты, дедупликация, retention и нагрузочный профиль |
| `SECURITY.md` | Секреты, логи, SSRF, heartbeat-токены, rate-limit |
| `DECISIONS.md` | Почему stdlib / pgx / self-hosted / `BASE_PATH`, правило новых зависимостей |
| `PLAN.md` | Полная дорожная карта: завершённый MVP, TCP, регионы, трекер, агент и релиз |
| `PROJECT_MAP.md` | Дерево репо и ответственность каждой папки |
| `SERVER_INSTALL.md`, `BACKUP.md` | Деплой на отдельный сервер, бэкапы |
| `ROUTES_AUDIT.md` | Аудит маршрутов и прав доступа |
| `AI_WORKLOG.md` | Журнал AI-изменений — дополнять после значимой задачи |

## Архитектура

Поток в `main.go`: `config.Load → postgres.Open → migrations.Up → services → Scheduler.Run (goroutine) → httpserver.NewApplication`. Graceful shutdown — 10 с на `SIGINT/SIGTERM`.

- `internal/config` — env: `HTTP_ADDR` (default `:8080`), обязательный `DATABASE_URL`, `PUBLIC_ORIGIN` (только scheme+host, без пути), `BASE_PATH` (например `/uptimec`, пусто = корень), `SESSION_COOKIE_SECURE` (default `true`, `false` только для локального HTTP), `TELEGRAM_BOT_TOKEN` + `TELEGRAM_CHAT_ID` только парой. `.env` автоматически не загружается.
- `internal/httpserver` — все маршруты строятся через `routePath(BASE_PATH, ...)`: `GET /health`, `GET /ready` (2 с таймаут, `503` без БД), `/api/v1/auth/*`, `/api/v1/projects/*`, `/api/v1/heartbeat/*`, встроенный Dashboard из `static/index.html` (без Node.js).
- `internal/auth` — email+пароль (Argon2id, 12 символов–128 байт), сессия 7 дней в `HttpOnly; SameSite=Lax` cookie `uptime_session`. В БД только SHA-256-хеш токена. Первый пользователь = владелец, дальше `registration_closed`. Rate-limit in-memory на процесс: register 5/мин, login 10/мин с IP.
- `internal/projects` — актуальная модель: проект-контейнер (сайт/API/игра/бот) + monitors `http` / `tcp` / `heartbeat`. Удаление мягкое (`deleted_at`), требует заголовка `X-Confirm-Delete: true`. Список использует cursor pagination.
- `internal/monitoring` — планировщик через `FOR UPDATE SKIP LOCKED` (`next_check_at` = защита от двойного захвата), безопасные HTTP/TCP-checkers (DNS/IP-проверка перед соединением и redirect, anti-SSRF), heartbeat (`204` и для неизвестного токена), история/сводки и Telegram на открытие/закрытие инцидента.
- `internal/pagination` — непрозрачные base64url-курсоры, привязанные хешем к владельцу и фильтрам запроса.
- `internal/sites` — legacy, основной сервер его не регистрирует. Не развивать, только понимать миграцию `000003`.
- `internal/netpolicy`, `identity`, `postgres`, `migrations` — SSRF-политика URL, UUID v4, `pgxpool`, SQL-миграции встроены в бинарник и применяются при старте под advisory lock. Применённые миграции не править — только новым файлом с возрастающим номером.
- `api/v1/ui/analytics.go` — пустая заглушка, игнорировать.

## Команды

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
DATABASE_URL=postgresql://localhost/uptime_control SESSION_COOKIE_SECURE=false go run .
curl -i http://localhost:8080/health
curl -i http://localhost:8080/ready
```

## ЗАПРЕЩЕНО без явного разрешения пользователя

Добавлять зависимости — запрещено без явного разрешения пользователя. Это касается Go-библиотек, внешних сервисов, runtime-компонентов, новых языков. Перед любым таким шагом нужен GitHub issue (назначение, альтернативы, лицензия, влияние на размер/обновления, риски безопасности) — см. `docs/DECISIONS.md`, `docs/SECURITY.md`. Молчание ≠ согласие: спрашивать и ждать ответа.

Дополнительно без отдельного решения нельзя: править применённые миграции, класть секреты (`DATABASE_URL`, токены, heartbeat-токены) в код/логи/docs/Git, логировать пароли/cookie/строки подключения, отключать авторизацию или SSRF-проверки «для упрощения».

## Коммиты — маленькие

Одно изменение = один коммит. Планируется несколько изменений — делать несколько коммитов по ходу выполнения, а не один большой в конце. Не смешивать фичу + фикс + docs в одном коммите. Коммитить/пушить только по явной просьбе.

Пользователь явно разрешил постоянный Git-процесс для этой рабочей копии: перед началом каждой новой задачи выполнять `git pull --ff-only origin main`; после каждого завершённого изменения создавать коммит от `tblgrihtml-rgb <tblgrihtml@gmail.com>` и сразу выполнять `git push origin main`. Пустые коммиты без изменений файлов не создавать.

## Грабли

- Владелец берётся только из сессии; запросы к чужому ресурсу отвечают `404` (не `403`). В SQL фильтровать одновременно по `id` и `user_id`.
- `PUBLIC_ORIGIN=https://igra.ru` + `BASE_PATH=/uptimec` — не склеивать origin с путём; cookie `Path` = `BASE_PATH`.
- JSON-телa ≤16 КиБ, неизвестные поля отклоняются; mutating auth-запросы проверяют `Origin`.
- Heartbeat: сырой токен возвращается один раз при создании, в БД только SHA-256-хеш; reverse proxy не должен писать полный путь `/api/v1/heartbeat/{token}` в access log.
- `uptime_checks` — самая быстрорастущая таблица; долгого хранения/агрегации пока нет.
