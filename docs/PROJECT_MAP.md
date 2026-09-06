# Карта проекта

Дата актуализации: 2026-09-06

## Текущее дерево

```text
UptimeControl/
├── api/
│   └── v1/
│       └── ui/
│           └── analytics.go
├── docs/
│   ├── API.md
│   ├── AI_WORKLOG.md
│   ├── BACKUP.md
│   ├── DATABASE.md
│   ├── DECISIONS.md
│   ├── PLAN.md
│   ├── PROJECT_MAP.md
│   ├── PROJECT_STATE.md
│   ├── ROUTES_AUDIT.md
│   ├── SERVER_INSTALL.md
│   ├── SETUP.md
│   ├── SECURITY.md
│   └── VERIFICATION.md
├── internal/
│   ├── auth/
│   │   ├── auth.go
│   │   ├── auth_test.go
│   │   ├── password.go
│   │   ├── password_test.go
│   │   ├── postgres_store.go
│   │   ├── token.go
│   │   └── token_test.go
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── httpserver/
│   │   ├── auth_handlers.go
│   │   ├── auth_handlers_test.go
│   │   ├── dashboard.go
│   │   ├── heartbeat_handler.go
│   │   ├── project_handlers.go
│   │   ├── project_handlers_test.go
│   │   ├── rate_limiter.go
│   │   ├── rate_limiter_test.go
│   │   ├── server.go
│   │   ├── server_test.go
│   │   ├── site_handlers.go
│   │   ├── site_handlers_test.go
│   │   └── static/index.html
│   ├── identity/
│   │   ├── uuid.go
│   │   └── uuid_test.go
│   ├── migrations/
│   │   ├── sql/
│   │   │   ├── 000001_initial_schema.up.sql
│   │   │   ├── 000002_unique_active_site_url.up.sql
│   │   │   ├── 000003_projects_and_monitors.up.sql
│   │   │   ├── 000004_project_webhooks.up.sql
│   │   │   └── README.md
│   │   └── migrations.go
│   ├── monitoring/
│   │   ├── checker.go
│   │   ├── notify.go
│   │   ├── postgres_store.go
│   │   ├── scheduler.go
│   │   ├── telegram.go
│   │   ├── webhook.go
│   │   └── webhook_test.go
│   ├── netpolicy/
│   │   ├── httpurl.go
│   │   ├── transport.go
│   │   └── transport_test.go
│   ├── postgres/
│   │   ├── postgres.go
│   │   └── postgres_test.go
│   ├── projects/
│   │   ├── postgres_store.go
│   │   ├── projects.go
│   │   ├── webhooks.go
│   │   └── webhooks_test.go
│   └── sites/
│       ├── postgres_store.go
│       ├── sites.go
│       └── sites_test.go
├── .env.example
├── .dockerignore
├── .gitignore
├── compose.yaml
├── Dockerfile
├── deploy/
│   ├── compose.env.example
│   └── uptime-control.service
├── go.mod
├── go.sum
├── LICENSE
├── main.go
└── README.md
```

Служебные `.DS_Store` и `.DS_Store.swp` в карту не включены, потому что они не относятся к исходному коду.

## Основные файлы и папки

### `/main.go`

Точка входа Go-приложения. Загружает конфигурацию, запускает HTTP-сервер и корректно завершает его по системному сигналу.

### `/api/v1/ui/analytics.go`

Заготовка пакета `analytics`. Реальной аналитики, маршрута или обработчика в файле пока нет.

### `/go.mod`

Описывает Go-модуль `github.com/MrArdek/UptimeControl` и зависимость `pgx/v5` для PostgreSQL. Контрольные суммы зависимостей находятся в `go.sum`.

### `/internal/config`

Читает и проверяет `HTTP_ADDR`, обязательный `DATABASE_URL`, `PUBLIC_ORIGIN`, `BASE_PATH` и `SESSION_COOKIE_SECURE`. Рядом находятся модульные тесты.

### `/internal/auth`

Содержит регистрацию, вход, проверку и отзыв сессий, Argon2id-хеширование паролей, генерацию токенов и PostgreSQL-хранилище. Рядом находятся модульные тесты.

### `/internal/sites`

Устаревшая реализация site API, оставленная временно для совместимости тестов и понимания миграции. Основной сервер эти маршруты больше не регистрирует.

### `/internal/projects`

Бизнес-правила и PostgreSQL-запросы проектов, их HTTP/heartbeat monitors и исходящих webhooks. Здесь находятся проверки владельца, мягкое удаление, одноразовая генерация heartbeat-токена и одноразовый webhook-секрет.

### `/internal/monitoring`

Планировщик заданий, безопасный HTTP checker, запись истории и инцидентов, приём heartbeat, Telegram-уведомления и диспетчер исходящих webhooks (`MultiNotifier` рассылает событие всем каналам, не ломая старые).

### `/internal/netpolicy`

Общая нормализация публичных HTTP/HTTPS URL, запрет внутренних/private IP и безопасный HTTP-транспорт для защиты от SSRF и DNS rebinding. Используется и checker, и доставкой webhooks.

### `/internal/identity`

Генерирует и проверяет UUID, общие для пользователей, сессий и сайтов.

### `/internal/httpserver`

Создаёт HTTP-сервер, встроенный Dashboard и маршруты health, auth, projects, monitors, history и heartbeat. Здесь также находятся Origin-проверка, лимит запросов и тесты.

### `/internal/postgres`

Создаёт пул соединений PostgreSQL через `pgxpool` и проверяет начальное подключение.

### `/internal/migrations`

Хранит встроенные SQL-миграции и применяет их при запуске приложения. Выполненные версии записываются в `schema_migrations`.

### `/.env.example`

Безопасный пример доступных переменных окружения без секретных значений.

### `/Dockerfile` и `/compose.yaml`

Воспроизводимая self-hosted-установка приложения вместе с отдельным PostgreSQL. Публично открывается только loopback-порт приложения через reverse proxy; порт PostgreSQL наружу не публикуется.

### `/deploy`

Пример переменных Docker Compose и шаблон systemd для ручной установки бинарного файла.

### `/README.md`

Главное продуктовое описание. Содержит цели сервиса, планируемые возможности и предполагаемый стек. Большинство описанных функций ещё не реализовано.

### `/LICENSE`

Текст лицензии GNU General Public License version 3.

### `/.gitignore`

Правила исключения сборочных файлов, `.env`, `.DS_Store` и других локальных артефактов из Git.

### `/docs`

Документация для разработки и передачи контекста.

- `PROJECT_STATE.md` — текущее фактическое состояние.
- `API.md` — документация реализованных HTTP-маршрутов.
- `DATABASE.md` — таблицы, связи, индексы и правила изменения схемы.
- `DECISIONS.md` — принятые технические решения и их ограничения.
- `PLAN.md` — этапы разработки и открытые вопросы.
- `BACKUP.md` — требования к резервным копиям и восстановлению.
- `ROUTES_AUDIT.md` — реализованные и предполагаемые маршруты, роли и проверки доступа.
- `SERVER_INSTALL.md` — установка и безопасный запуск на тестовом Linux-сервере.
- `SECURITY.md` — правила безопасной разработки.
- `AI_WORKLOG.md` — журнал значимых изменений, выполненных AI.
- `PROJECT_MAP.md` — этот файл, карта структуры проекта.
- `SETUP.md` — установка, настройка, запуск и проверка.
- `VERIFICATION.md` — актуальные команды проверки и результаты последнего прогона.

## Где искать важные вещи сейчас

| Область | Расположение | Состояние |
| --- | --- | --- |
| Точка входа | `main.go` | Запуск и завершение HTTP-сервера |
| Конфигурация | `internal/config` | Адреса, PostgreSQL, origin, base path и cookie |
| HTTP-сервер и Dashboard | `internal/httpserver` | UI, служебные, auth-, project- и heartbeat-маршруты |
| Авторизация | `internal/auth` | Регистрация, вход, сессии и Argon2id |
| Проекты | `internal/projects` | CRUD проектов и HTTP/heartbeat monitors |
| Мониторинг | `internal/monitoring` | Scheduler, HTTP checker, heartbeat, история, инциденты и Telegram |
| Сетевая безопасность | `internal/netpolicy` | URL, DNS/IP и SSRF-политика |
| Старый site API | `internal/sites` | Не регистрируется основной точкой запуска |
| PostgreSQL | `internal/postgres` | Пул соединений и проверка подключения |
| Миграции | `internal/migrations` | Начальная схема применяется автоматически |
| Аналитика | `api/v1/ui/analytics.go` | Пустая заготовка |
| Зависимости и версия Go | `go.mod` | Создано |
| Требования к продукту | `README.md` | Подробное описание |
| План разработки | `docs/PLAN.md` | Создан |
| Безопасность | `docs/SECURITY.md` | Создан |
| API | `docs/API.md` | Полное MVP API |
| База данных | `docs/DATABASE.md` | Users, projects, monitors, checks и incidents |
| Frontend | `internal/httpserver/static/index.html` | Встроенный адаптивный Dashboard |
| Тесты | `internal/**/*_test.go` | Unit и PostgreSQL integration tests |

## Предлагаемая будущая структура

Следующие пути ещё не существуют. Это ориентир, который нужно подтвердить при создании каркаса приложения:

```text
cmd/                 точки входа backend, check node и server agent
web/                 возможный расширенный frontend после встроенного MVP Dashboard
tracker/             JavaScript-трекер
agent/               код Server Agent, если он не вынесен в отдельный модуль
```

Не следует создавать всю эту структуру заранее: папки нужно добавлять по мере появления реальной логики.

## Что лучше не трогать без необходимости

- `.git/` — внутренние данные Git.
- `LICENSE` — изменение лицензии требует отдельного решения.
- уже применённые миграции после их появления — исправления делаются новыми миграциями.
- файлы с секретами и production-конфигурацией — их нельзя добавлять в Git.

## Правило обновления карты

При появлении новой важной папки, приложения, базы данных, frontend или набора тестов нужно добавить сюда краткое описание: что там находится и за какую часть системы отвечает.
