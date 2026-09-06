# API

Дата актуализации: 2026-09-06

Базовый адрес при настройках по умолчанию: `http://localhost:8080`.

Если задан `BASE_PATH=/uptimec`, этот префикс добавляется ко всем маршрутам. Например, health endpoint будет доступен по адресу `https://igra.ru/uptimec/health`, а регистрация — по `https://igra.ru/uptimec/api/v1/auth/register`.

Маршруты `/sites` оставлены только в исходном коде для совместимости тестов и не регистрируются основной точкой запуска. Актуальный API использует проекты и отдельные monitors; данные старых `sites` переносятся миграцией.

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

Возвращает до 100 проектов владельца вместе с monitors и их последним состоянием.

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

### GET, PATCH, DELETE /api/v1/projects/{projectID}

- `GET` возвращает проект владельца;
- `PATCH` изменяет `name` и/или `description`;
- `DELETE` мягко удаляет проект и выключает monitors, требуется `X-Confirm-Delete: true`.

### POST /api/v1/projects/{projectID}/monitors

Добавляет HTTP или heartbeat monitor к существующему проекту. Формат совпадает с объектом `monitor` из создания проекта.

### PATCH, DELETE /api/v1/projects/{projectID}/monitors/{monitorID}

- `PATCH` изменяет название, URL, интервал, таймаут или состояние `enabled`;
- `DELETE` мягко удаляет monitor и требует `X-Confirm-Delete: true`.

### GET /api/v1/projects/{projectID}/monitors/{monitorID}/checks

Возвращает историю проверок от новых к старым. Параметр `limit` ограничен диапазоном до 500, значение по умолчанию — 100.

### GET /api/v1/projects/{projectID}/monitors/{monitorID}/incidents

Возвращает историю инцидентов. `resolved_at: null` означает, что инцидент открыт.

### POST /api/v1/heartbeat/{secretToken}

Публичный endpoint с секретным высокоэнтропийным токеном. Предназначен для Telegram-ботов, cron и фоновых сервисов.

Успешный ответ: `204 No Content`. Неизвестный токен также получает `204`, чтобы API не позволяло проверять существование токенов перебором.

Пример:

```bash
curl --fail -X POST 'https://uptime.example.com/api/v1/heartbeat/SECRET_TOKEN'
```

Сигнал нужно отправлять чаще настроенного интервала. Если он не приходит вовремя, открывается инцидент; следующий heartbeat закрывает его.

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
