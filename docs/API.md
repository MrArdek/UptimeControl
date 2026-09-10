# API

Дата актуализации: 2026-09-06

Базовый адрес при настройках по умолчанию: `http://localhost:8080`.

Если задан `BASE_PATH=/uptimec`, этот префикс добавляется ко всем маршрутам. Например, health endpoint будет доступен по адресу `https://igra.ru/uptimec/health`, а регистрация — по `https://igra.ru/uptimec/api/v1/auth/register`.

Маршруты `/sites` оставлены только в исходном коде для совместимости тестов и не регистрируются основной точкой запуска. Актуальный API использует проекты и отдельные monitors; данные старых `sites` переносятся миграцией.

## Контракты будущих API полного продукта

Версионирование, batch-приём, дедупликация, время, пагинация, ошибки и границы доверия для будущих Monitoring, Analytics и Metrics API зафиксированы в [DATA_CONTRACTS.md](DATA_CONTRACTS.md). Эти маршруты появятся на этапах 12, 13 и 17 и до реализации не считаются доступными.

Новые списки используют cursor pagination с `limit` до 500, фильтрами `from` включительно и `to` исключительно. Точки временного ряда различают `null` (нет наблюдения) и `0` (подтверждённый ноль). API отчётов не смешивает синтетическое время monitor, браузерную загрузку и серверные запросы.

## GET /health

Назначение:

Проверяет, что процесс приложения запущен и HTTP-сервер отвечает.

Авторизация:

Не требуется.

Успешный ответ:

- Статус: `200 OK`.
- Content-Type: `application/json; charset=utf-8`.
- Ответ запрещено кэшировать заголовком `Cache-Control: no-store`.

```json
{
  "status": "ok"
}
```

Ошибки:

- `405 Method Not Allowed` — использован любой метод кроме `GET`.

Ограничения:

- Endpoint проверяет только работу HTTP-процесса.
- Ответ намеренно не содержит версии, конфигурации или других внутренних сведений.

## GET /ready

Назначение:

Проверяет, что приложение может выполнять запросы к PostgreSQL и готово обслуживать прикладные запросы.

Авторизация:

Не требуется.

Успешный ответ:

- Статус: `200 OK`.

```json
{
  "status": "ready"
}
```

Ошибки:

- `503 Service Unavailable` — PostgreSQL недоступен или не ответил за 2 секунды.
- `405 Method Not Allowed` — использован любой метод кроме `GET`.

Ответ при недоступной базе:

```json
{
  "status": "unavailable"
}
```

Внутренняя ошибка подключения намеренно не возвращается клиенту.

## Авторизация

Сессия передаётся в cookie `uptime_session`. JavaScript в браузере не имеет доступа к cookie благодаря флагу `HttpOnly`.

Для запросов из браузера frontend и API должны использовать один origin или согласованный `PUBLIC_ORIGIN`.

### POST /api/v1/auth/register

Назначение:

Создаёт пользователя и сразу открывает сессию.

Авторизация:

Не требуется.

Body:

```json
{
  "email": "user@example.com",
  "password": "<пароль длиной от 12 символов>"
}
```

Ограничения:

- тело должно иметь `Content-Type: application/json`;
- максимальный размер — 16 КиБ;
- пароль должен содержать от 12 символов и занимать не более 128 байт;
- пробелы в начале и конце пароля запрещены;
- не более 5 попыток в минуту с одного IP одного процесса.

Успешный ответ:

- Статус: `201 Created`.
- Заголовок `Set-Cookie` содержит сессию.

```json
{
  "user": {
    "id": "00000000-0000-4000-8000-000000000000",
    "email": "user@example.com",
    "created_at": "2026-09-05T18:00:00Z"
  }
}
```

Ошибки:

- `400 invalid_request` — неверный JSON или Content-Type;
- `400 invalid_email` — неверный email;
- `400 invalid_password` — пароль не соответствует требованиям;
- `403 invalid_origin` — браузерный Origin не разрешён;
- `409 email_taken` — email уже зарегистрирован;
- `403 registration_closed` — владелец self-hosted-экземпляра уже создан;
- `429 rate_limited` — превышен лимит запросов;
- `500 internal_error` — внутренняя ошибка без раскрытия деталей.

### POST /api/v1/auth/login

Назначение:

Проверяет email и пароль, затем создаёт новую сессию.

Авторизация:

Не требуется.

Body имеет тот же формат, что и регистрация.

Успешный ответ:

- Статус: `200 OK`.
- Заголовок `Set-Cookie` содержит новую сессию.
- Тело содержит объект `user`, как при регистрации.

Ошибки:

- `400 invalid_request`;
- `401 invalid_credentials` — единая ошибка для неверного email или пароля;
- `403 invalid_origin`;
- `429 rate_limited` — более 10 попыток в минуту с одного IP одного процесса;
- `500 internal_error`.

### GET /api/v1/auth/me

Назначение:

Возвращает пользователя действующей сессии.

Авторизация:

Требуется cookie `uptime_session`.

Успешный ответ:

- Статус: `200 OK`.
- Тело содержит объект `user`.

Ошибки:

- `401 unauthorized` — cookie отсутствует, сессия истекла или отозвана;
- `500 internal_error`.

### POST /api/v1/auth/logout

Назначение:

Отзывает текущую сессию и удаляет cookie.

Авторизация:

Требуется cookie `uptime_session`.

Успешный ответ:

- Статус: `204 No Content`.

Ошибки:

- `401 unauthorized` — действующая сессия отсутствует;
- `403 invalid_origin` — браузерный Origin не разрешён;
- `500 internal_error`.

## Проекты и monitors

Все маршруты управления требуют действующую cookie-сессию. Первый monitor создаётся вместе с проектом.

### GET /api/v1/projects

Возвращает проекты владельца вместе с monitors и их последним состоянием. Параметры:

- `limit` — от 1 до 500, по умолчанию 100;
- `cursor` — непрозрачное значение `next_cursor` из предыдущего ответа.

Ответ имеет форму `{"projects": [...], "next_cursor": "..."}`. `next_cursor` равен `null`, когда записей больше нет. Курсор привязан к владельцу и не принимается в другой сессии.

Состояние monitor содержится в полях:

- `last_checked_at`;
- `last_available`;
- `last_status_code`;
- `last_response_time_ms`;
- `last_error`.

До первой проверки эти поля имеют значение `null`.

### POST /api/v1/projects

Создаёт проект и первый monitor.

HTTP-пример:

```json
{
  "name": "Public API",
  "description": "Основной backend",
  "monitor": {
    "type": "http",
    "name": "Health endpoint",
    "url": "https://api.example.com/health",
    "check_interval_seconds": 60,
    "timeout_seconds": 10
  }
}
```

Heartbeat-пример для Telegram-бота, cron или worker без frontend:

```json
{
  "name": "Telegram bot",
  "monitor": {
    "type": "heartbeat",
    "name": "Worker loop",
    "check_interval_seconds": 60
  }
}
```

При создании heartbeat monitor ответ один раз содержит `heartbeat_token`. Сырой токен не сохраняется в PostgreSQL и не возвращается в последующих списках.

TCP-пример:

```json
{
  "name": "PostgreSQL",
  "monitor": {
    "type": "tcp",
    "name": "Public database port",
    "target": "db.example.com:5432",
    "check_interval_seconds": 60,
    "timeout_seconds": 5
  }
}
```

TCP-цель задаётся как `host:port`. Локальные, private, link-local, multicast, unspecified, CGNAT и служебные адреса отклоняются. DNS и полученные IP повторно проверяются непосредственно перед соединением.

### GET, PATCH, DELETE /api/v1/projects/{projectID}

- `GET` возвращает проект владельца;
- `PATCH` изменяет `name` и/или `description`;
- `DELETE` мягко удаляет проект и выключает monitors, требуется `X-Confirm-Delete: true`.

### POST /api/v1/projects/{projectID}/monitors

Добавляет HTTP, TCP или heartbeat monitor к существующему проекту. Формат совпадает с объектом `monitor` из создания проекта.

### PATCH, DELETE /api/v1/projects/{projectID}/monitors/{monitorID}

- `PATCH` изменяет название, URL или TCP-цель, интервал, таймаут или состояние `enabled`;
- `DELETE` мягко удаляет monitor и требует `X-Confirm-Delete: true`.

### GET /api/v1/projects/{projectID}/monitors/{monitorID}/checks

Возвращает историю проверок от новых к старым. Параметры `limit` и `cursor` работают как в списке проектов. Фильтр `from` включителен, `to` исключителен; оба значения имеют формат RFC 3339. По умолчанию возвращаются последние 24 часа, максимальный период — 31 день.

Ответ содержит `checks`, `next_cursor`, а также фактические `from` и `to`. При запросе следующей страницы нужно повторить эти точные `from` и `to`, потому что курсор привязан хешем к monitor и периоду. У TCP и heartbeat поле `status_code` равно `null`.

### GET /api/v1/projects/{projectID}/monitors/{monitorID}/incidents

Возвращает историю инцидентов с теми же `limit`, `cursor`, `from` и `to`. Инцидент включается, если пересекает выбранный период. `resolved_at: null` означает, что инцидент открыт; `duration_seconds` содержит текущую или окончательную длительность.

### GET /api/v1/projects/{projectID}/monitors/{monitorID}/summary

Возвращает сводку за период `from`/`to` (по умолчанию последние 24 часа, максимум 31 день):

- `status`: `unknown`, `up`, `down`, `paused` или `stale`;
- `uptime_percent` — доля доступного времени среди наблюдаемого либо `null` без наблюдений;
- `coverage_percent` — доля периода, покрытая свежими результатами;
- наблюдаемая и доступная длительность в секундах;
- число проверок, среднее и пиковое response time в миллисекундах;
- последний инцидент к концу выбранного периода и его длительность.

Результат проверки считается свежим до `max(2 × check_interval, 5 минут)`. После этой границы пропуск данных получает состояние `stale` и не считается падением. Первый неуспешный результат открывает инцидент, первый последующий успешный закрывает его.

### POST /api/v1/heartbeat/{secretToken}

Публичный endpoint с секретным высокоэнтропийным токеном. Предназначен для Telegram-ботов, cron и фоновых сервисов.

Успешный ответ: `204 No Content`. Неизвестный токен также получает `204`, чтобы API не позволяло проверять существование токенов перебором.

Пример:

```bash
curl --fail -X POST 'https://uptime.example.com/api/v1/heartbeat/SECRET_TOKEN'
```

Сигнал нужно отправлять чаще настроенного интервала. Если он не приходит вовремя, открывается инцидент; следующий heartbeat закрывает его.

### Webhooks: исходящие уведомления

Каждый проект может иметь до 5 webhook-эндпоинтов. При открытии (`down`) и закрытии (`recovered`) инцидента событие транзакционно попадает в постоянную очередь, а отдельный worker отправляет подписанный `POST` на каждый включённый URL, подписанный на это событие. Работает рядом с Telegram.

```bash
curl -X POST "$ORIGIN/api/v1/projects/$PROJECT_ID/webhooks" \
  -H 'Content-Type: application/json' \
  -H "Cookie: uptime_session=$SESSION" \
  -d '{"url":"https://hooks.example.com/uptime","events":"down,recovered"}'
```

- `url` — публичный абсолютный HTTP/HTTPS URL (та же SSRF-политика, что и у monitors);
- `events` — подмножество `down,recovered` через запятую, по умолчанию оба события.

Успех: `201 Created`, заголовок `Location` и объект `webhook`. Поле `secret` присутствует **только** в этом ответе — сырой секрет показывается один раз, в списках его нет:

```json
{
  "webhook": {
    "id": "00000000-0000-4000-8000-000000000002",
    "project_id": "00000000-0000-4000-8000-000000000001",
    "url": "https://hooks.example.com/uptime",
    "events": "down,recovered",
    "enabled": true,
    "secret": "показать один раз и сохранить",
    "created_at": "2026-09-06T12:00:00Z",
    "updated_at": "2026-09-06T12:00:00Z"
  }
}
```

- `GET /api/v1/projects/{projectID}/webhooks` — список без секретов;
- `PATCH /api/v1/projects/{projectID}/webhooks/{webhookID}` — меняет `events` и/или `enabled`;
- `DELETE /api/v1/projects/{projectID}/webhooks/{webhookID}` — мягкое удаление, требуется `X-Confirm-Delete: true`, ответ `204`.

Ошибки совпадают с проектами, плюс `400 invalid_events` (события вне `down,recovered`) и `409 webhook_limit` (у проекта уже 5 webhooks). Чужой проект возвращает тот же `404 project_not_found`.

Формат доставки:

```json
{
  "event_id": "00000000-0000-4000-8000-000000000010",
  "event": "down",
  "project_id": "00000000-0000-4000-8000-000000000001",
  "project_name": "Public API",
  "monitor_id": "00000000-0000-4000-8000-000000000003",
  "monitor_name": "Health endpoint",
  "url": "https://api.example.com/health",
  "cause": "connection refused",
  "occurred_at": "2026-09-06T12:00:00Z"
}
```

Заголовки каждого запроса: `Content-Type: application/json`, `X-UptimeControl-Event: down|recovered|test`, `X-UptimeControl-Event-ID: <event_id>`, `X-UptimeControl-Signature: sha256=<HMAC-SHA256(secret, body)>`. Подпись проверяйте так:

```python
hmac.compare_digest("sha256=" + hmac.new(secret, body, hashlib.sha256).hexdigest(),
                     request.headers["X-UptimeControl-Signature"])
```

Доставка имеет семантику **как минимум один раз**. Если получатель принял POST, но worker не смог зафиксировать успех до завершения аренды, событие будет отправлено повторно. Получатель должен сохранять `event_id` и отвечать `2xx` на уже обработанный ID без повторения бизнес-действия.

### GET /api/v1/projects/{projectID}/notifications

Возвращает журнал событий доставки текущего владельца. Поддерживает `limit` от 1 до 500 и `cursor`. Каждый элемент содержит стабильный `id`, тип, monitor, состояние `pending|processing|delivered|failed`, число попыток, время следующей попытки, безопасный `last_error` и `attempt_history`.

История не содержит URL назначения, Telegram token, chat ID или webhook secret. Возможные коды ошибок ограничены значениями вроде `channel_delivery_failed` и `delivery_timeout`.

### POST /api/v1/projects/{projectID}/notifications/test

Создаёт асинхронное событие `test` и отвечает `202 Accepted`. Событие проходит ту же очередь, Telegram и все включённые webhooks, но не открывает инцидент.

### POST /api/v1/projects/{projectID}/notifications/{notificationID}/retry

Повторно ставит в очередь только событие со статусом `failed`. Требует cookie, допустимый Origin и заголовок `X-Confirm-Retry: true`; успех — `204 No Content`. Чужое, несуществующее или ещё активное событие получает одинаковый `404 notification_not_found`.

Worker выполняет до 8 циклов доставки с задержками `5s`, `30s`, `2m`, `10m`, `30m`, `1h`, `2h`; один цикл ограничен 20 секундами. До четырёх событий доставляются параллельно. Состояние `processing` с просроченной минутной арендой снова доступно после рестарта. Один queue worker не захватывает событие, уже арендованное другим worker.

Исходящие запросы идут через тот же безопасный транспорт, что и проверки: без proxy, с DNS/IP-проверкой каждого соединения, максимум 3 редиректа только на публичные URL и сетевой таймаут 10 секунд. Один вызов канала делает один запрос; повторы и общий 20-секундный лимит управляются постоянной очередью. Webhook-исход пишется в `webhook_deliveries`, а исход полного fan-out — в `notification_attempts`.

## Устаревший API сайтов

Следующие маршруты описывают предыдущую модель и не регистрируются основной self-hosted-точкой запуска. Они сохранены временно для понимания миграции и будут удалены после стабилизации API проектов.

## Сайты

Все маршруты требуют действующую cookie-сессию. Пользователь получает только собственные сайты; чужой и несуществующий идентификатор возвращают одинаковый `404`.

### GET /api/v1/sites

Возвращает до 100 активных сайтов текущего пользователя, от новых к старым.

```json
{
  "sites": [
    {
      "id": "00000000-0000-4000-8000-000000000001",
      "name": "Main site",
      "url": "https://example.com",
      "check_interval_seconds": 60,
      "enabled": true,
      "created_at": "2026-09-05T18:00:00Z",
      "updated_at": "2026-09-05T18:00:00Z"
    }
  ]
}
```

Пагинация пока не реализована.

### POST /api/v1/sites

Создаёт сайт для текущего пользователя.

```json
{
  "name": "Main site",
  "url": "https://example.com",
  "check_interval_seconds": 60
}
```

- `name` — от 1 до 100 символов;
- `url` — публичный абсолютный HTTP/HTTPS URL, максимум 2048 байт;
- `check_interval_seconds` — необязательное число от 30 до 86400, по умолчанию 60.

Успех: `201 Created`, заголовок `Location` и объект `site`.

Ошибки:

- `400 invalid_request`, `invalid_name`, `invalid_url` или `invalid_interval`;
- `401 unauthorized`;
- `403 invalid_origin`;
- `409 site_exists` — такой активный URL уже есть у пользователя;
- `500 internal_error`.

### GET /api/v1/sites/{siteID}

Возвращает один активный сайт текущего пользователя.

- `401 unauthorized`;
- `404 site_not_found` — сайт отсутствует, удалён или принадлежит другому пользователю;
- `500 internal_error`.

### PATCH /api/v1/sites/{siteID}

Изменяет одно или несколько полей:

```json
{
  "name": "Updated site",
  "url": "https://example.com/health",
  "check_interval_seconds": 300,
  "enabled": false
}
```

Успех: `200 OK` и обновлённый объект `site`.

Дополнительная ошибка `400 empty_update` означает, что не передано ни одного изменяемого поля. Остальные проверки совпадают с созданием.

### DELETE /api/v1/sites/{siteID}

Мягко удаляет и выключает сайт. История остаётся в базе.

Обязательный заголовок:

```text
X-Confirm-Delete: true
```

Успех: `204 No Content`.

Ошибки:

- `400 confirmation_required` — отсутствует явное подтверждение;
- `401 unauthorized`;
- `403 invalid_origin`;
- `404 site_not_found`;
- `500 internal_error`.
