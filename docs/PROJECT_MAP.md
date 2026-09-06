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
│   │   ├── rate_limiter.go
│   │   ├── rate_limiter_test.go
│   │   ├── server.go
│   │   ├── server_test.go
│   │   ├── site_handlers.go
│   │   └── site_handlers_test.go
│   ├── identity/
│   │   ├── uuid.go
│   │   └── uuid_test.go
│   ├── migrations/
│   │   ├── sql/
│   │   │   ├── 000001_initial_schema.up.sql
│   │   │   ├── 000002_unique_active_site_url.up.sql
│   │   │   └── README.md
│   │   └── migrations.go
│   ├── postgres/
│   │   ├── postgres.go
│   │   └── postgres_test.go
│   └── sites/
│       ├── postgres_store.go
│       ├── sites.go
│       └── sites_test.go
├── .env.example
├── .gitignore
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

Содержит текущие бизнес-правила HTTP-целей, проверку URL и PostgreSQL-запросы с обязательным владельцем. Название `sites` временное до миграции на `projects` и `monitors`.

### `/internal/identity`

Генерирует и проверяет UUID, общие для пользователей, сессий и сайтов.

### `/internal/httpserver`

Создаёт HTTP-сервер, задаёт таймауты и регистрирует служебные, auth- и site-маршруты. Здесь находятся HTTP-обработчики, Origin-проверка, лимит запросов и тесты.

### `/internal/postgres`

Создаёт пул соединений PostgreSQL через `pgxpool` и проверяет начальное подключение.

### `/internal/migrations`

Хранит встроенные SQL-миграции и применяет их при запуске приложения. Выполненные версии записываются в `schema_migrations`.

### `/.env.example`

Безопасный пример доступных переменных окружения без секретных значений.

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
| HTTP-сервер | `internal/httpserver` | Служебные и auth-маршруты |
| Авторизация | `internal/auth` | Регистрация, вход, сессии и Argon2id |
| HTTP-цели (временно sites) | `internal/sites` | CRUD, проверка владельца и URL; ожидает миграции к monitors |
| PostgreSQL | `internal/postgres` | Пул соединений и проверка подключения |
| Миграции | `internal/migrations` | Начальная схема применяется автоматически |
| Аналитика | `api/v1/ui/analytics.go` | Пустая заготовка |
| Зависимости и версия Go | `go.mod` | Создано |
| Требования к продукту | `README.md` | Подробное описание |
| План разработки | `docs/PLAN.md` | Создан |
| Безопасность | `docs/SECURITY.md` | Создан |
| API | `docs/API.md` | Служебные и auth-маршруты |
| База данных | `docs/DATABASE.md` | PostgreSQL подключён, начальная схема создана |
| Frontend | — | Не создан |
| Тесты | `internal/**/*_test.go` | Конфигурация, auth, сайты и HTTP |

## Предлагаемая будущая структура

Следующие пути ещё не существуют. Это ориентир, который нужно подтвердить при создании каркаса приложения:

```text
cmd/                 точки входа backend, check node и server agent
internal/monitoring/ планировщик, проверки и инциденты
internal/projects/   проекты и их monitors после миграции модели
web/                 frontend на TypeScript/React или Next.js
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
