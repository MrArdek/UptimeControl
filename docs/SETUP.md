# Запуск проекта

Дата актуализации: 2026-09-06

## Требования

- Go 1.27 или новее в пределах совместимости проекта.
- PostgreSQL версии 14–18.
- Свободный TCP-порт, по умолчанию `8080`.

Проект проверен на Go 1.27.1 и PostgreSQL 18.6.

На macOS инструменты можно установить через Homebrew:

```bash
brew install go postgresql@18
```

Запуск локального PostgreSQL как фонового сервиса:

```bash
brew services start postgresql@18
```

Создание пустой локальной базы выполняется один раз:

```bash
/opt/homebrew/opt/postgresql@18/bin/createdb uptime_control
```

## Настройка окружения

Для локальных настроек можно создать `.env` на основе `.env.example`. Само приложение читает переменные окружения, но пока не загружает `.env` автоматически.

Доступные переменные:

```text
HTTP_ADDR=:8080
DATABASE_URL=postgresql://localhost/uptime_control
PUBLIC_ORIGIN=http://localhost:8080
BASE_PATH=
SESSION_COOKIE_SECURE=false
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=
```

- `HTTP_ADDR` — адрес и порт HTTP-сервера.
- Если переменная не задана или пуста, используется `:8080`.
- Для доступа только с текущего компьютера можно использовать `127.0.0.1:8080`.
- `DATABASE_URL` — обязательная строка подключения к PostgreSQL.
- Реальный пароль БД хранится только в локальном `.env` или менеджере секретов.
- Приложение не выводит `DATABASE_URL` в лог.
- `PUBLIC_ORIGIN` — внешний адрес frontend/API без пути; по умолчанию `http://localhost:8080`.
- `BASE_PATH` — необязательный путь установки, например `/uptimec`. Пустое значение означает корень сайта.
- `SESSION_COOKIE_SECURE` по умолчанию `true`. Значение `false` допустимо только для локального HTTP.
- `TELEGRAM_BOT_TOKEN` и `TELEGRAM_CHAT_ID` — необязательная пара для уведомлений. Нужно задать оба значения либо оставить оба пустыми.

Для адреса `https://igra.ru/uptimec` значения разделяются так:

```text
PUBLIC_ORIGIN=https://igra.ru
BASE_PATH=/uptimec
SESSION_COOKIE_SECURE=true
```

`.env` не загружается автоматически. Варианты локального запуска:

```bash
DATABASE_URL=postgresql://localhost/uptime_control SESSION_COOKIE_SECURE=false go run .
```

Или сначала экспортировать переменные из локального `.env` средствами оболочки.

## Запуск

Из корня проекта:

```bash
DATABASE_URL=postgresql://localhost/uptime_control SESSION_COOKIE_SECURE=false go run .
```

При первом запуске приложение подключится к PostgreSQL и применит ещё не выполненные миграции. После этого сервер будет доступен по адресу `http://localhost:8080`.

Если задан `BASE_PATH=/uptimec`, локальные маршруты будут начинаться с `http://localhost:8080/uptimec`.

## Проверка состояния

```bash
curl -i http://localhost:8080/health
curl -i http://localhost:8080/ready
```

Ожидаемый статус: `200 OK`.

Ожидаемое тело:

```json
{"status":"ok"}
```

`/ready` дополнительно должен вернуть `{"status":"ready"}`. Если PostgreSQL перестанет отвечать, этот маршрут вернёт `503 Service Unavailable`.

## Запуск тестов

```bash
go test ./...
go test -race ./...
go vet ./...
```

## Сборка

```bash
go build -o uptime-control .
```

Собранный бинарный файл запускается с теми же переменными окружения:

```bash
DATABASE_URL=postgresql://localhost/uptime_control ./uptime-control
```

При локальном HTTP добавьте `SESSION_COOKIE_SECURE=false`. На HTTPS-сервере оставьте значение `true`.

## Корректное завершение

Нажатие `Ctrl+C` отправляет процессу сигнал завершения. Сервер перестаёт принимать новые соединения и получает до 10 секунд на завершение текущих запросов.

## Частые ошибки

### `command not found: go`

Go не установлен или его исполняемый файл отсутствует в `PATH`. Нужно установить согласованную версию Go и перезапустить терминал.

### `address already in use`

Порт уже занят. Укажите другой адрес перед запуском:

```bash
HTTP_ADDR=:9090 go run .
```

### Ошибка формата `HTTP_ADDR`

Значение должно включать адрес и порт, например `:8080` или `127.0.0.1:8080`. Допустимый диапазон порта: от 1 до 65535.

### Ошибка `DATABASE_URL: is required`

Не задана строка подключения. Укажите `DATABASE_URL` в окружении процесса.

### Ошибка подключения к PostgreSQL

Проверьте, что PostgreSQL запущен, база создана, адрес доступен, а пользователь имеет права подключения и применения миграций. Не отправляйте полную строку подключения в публичные сообщения: она может содержать пароль.

## Проверка авторизации

Запустите приложение с `SESSION_COOKIE_SECURE=false`, затем в другом терминале создайте временное хранилище cookie:

```bash
UPTIME_COOKIE_JAR=$(mktemp)
```

Регистрация:

```bash
curl -i -c "$UPTIME_COOKIE_JAR" \
  -H 'Content-Type: application/json' \
  -H 'Origin: http://localhost:8080' \
  -d '{"email":"user@example.com","password":"example password value"}' \
  http://localhost:8080/api/v1/auth/register
```

Получение текущего пользователя:

```bash
curl -i -b "$UPTIME_COOKIE_JAR" \
  http://localhost:8080/api/v1/auth/me
```

Выход:

```bash
curl -i -b "$UPTIME_COOKIE_JAR" -c "$UPTIME_COOKIE_JAR" \
  -H 'Origin: http://localhost:8080' \
  -X POST http://localhost:8080/api/v1/auth/logout
```

Пока не удаляйте временный файл: он понадобится для проверки проектов. После всех проверок выполните:

```bash
unlink "$UPTIME_COOKIE_JAR"
```

## Проверка проектов и HTTP monitor

До удаления временного cookie-файла создайте проект:

```bash
curl -i -b "$UPTIME_COOKIE_JAR" \
  -H 'Content-Type: application/json' \
  -H 'Origin: http://localhost:8080' \
  -d '{"name":"Main API","monitor":{"type":"http","name":"Health","url":"https://example.com","check_interval_seconds":60,"timeout_seconds":10}}' \
  http://localhost:8080/api/v1/projects
```

Получите список:

```bash
curl -i -b "$UPTIME_COOKIE_JAR" \
  http://localhost:8080/api/v1/projects
```

Для удаления замените `PROJECT_ID` на идентификатор из ответа создания:

```bash
curl -i -b "$UPTIME_COOKIE_JAR" \
  -H 'Origin: http://localhost:8080' \
  -H 'X-Confirm-Delete: true' \
  -X DELETE http://localhost:8080/api/v1/projects/PROJECT_ID
```

Удаление без заголовка подтверждения отклоняется.

## Проверка Telegram-бота или сервиса без frontend

Создайте heartbeat-проект:

```bash
curl -i -b "$UPTIME_COOKIE_JAR" \
  -H 'Content-Type: application/json' \
  -H 'Origin: http://localhost:8080' \
  -d '{"name":"Telegram bot","monitor":{"type":"heartbeat","name":"Worker loop","check_interval_seconds":60}}' \
  http://localhost:8080/api/v1/projects
```

Ответ один раз содержит `heartbeat_token`. Подставьте его в запрос, который бот отправляет после успешного цикла:

```bash
curl --fail -X POST http://localhost:8080/api/v1/heartbeat/HEARTBEAT_TOKEN
```

Сигнал должен приходить чаще заданного интервала. Если он пропадёт, будет открыт инцидент; следующий сигнал закроет его.

## Dashboard

После запуска откройте `http://localhost:8080/` или путь из `BASE_PATH`. Первый зарегистрированный пользователь становится владельцем установки. Повторная публичная регистрация автоматически закрывается.

## Тестовый запуск на сервере

Текущую версию уже можно собрать и запустить на тестовом Linux-сервере при наличии Go 1.27+ и PostgreSQL 14–18:

```bash
go build -o uptime-control .
DATABASE_URL=postgresql://DB_HOST/DB_NAME HTTP_ADDR=127.0.0.1:8080 ./uptime-control
```

`DB_HOST` и `DB_NAME` нужно заменить настройками сервера. Пароль безопаснее передавать через менеджер секретов или защищённый файл окружения, а не записывать в командную историю.

Для запуска под существующим доменом добавьте `PUBLIC_ORIGIN` и `BASE_PATH`:

```bash
DATABASE_URL=postgresql://DB_HOST/DB_NAME \
HTTP_ADDR=127.0.0.1:8080 \
PUBLIC_ORIGIN=https://igra.ru \
BASE_PATH=/uptimec \
SESSION_COOKIE_SECURE=true \
./uptime-control
```

Это пока тестовый запуск. Перед публичным production-развёртыванием ещё нужны reverse proxy, TLS, отдельный системный пользователь, сервисный менеджер, firewall и резервные копии.
