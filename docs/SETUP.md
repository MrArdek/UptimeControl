# Запуск проекта

Дата актуализации: 2026-09-05

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
SESSION_COOKIE_SECURE=false
```

- `HTTP_ADDR` — адрес и порт HTTP-сервера.
- Если переменная не задана или пуста, используется `:8080`.
- Для доступа только с текущего компьютера можно использовать `127.0.0.1:8080`.
- `DATABASE_URL` — обязательная строка подключения к PostgreSQL.
- Реальный пароль БД хранится только в локальном `.env` или менеджере секретов.
- Приложение не выводит `DATABASE_URL` в лог.
- `PUBLIC_ORIGIN` — внешний адрес frontend/API без пути; по умолчанию `http://localhost:8080`.
- `SESSION_COOKIE_SECURE` по умолчанию `true`. Значение `false` допустимо только для локального HTTP.

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

После проверки удалите временный файл с токеном:

```bash
unlink "$UPTIME_COOKIE_JAR"
```

## Тестовый запуск на сервере

Текущую версию уже можно собрать и запустить на тестовом Linux-сервере при наличии Go 1.27+ и PostgreSQL 14–18:

```bash
go build -o uptime-control .
DATABASE_URL=postgresql://DB_HOST/DB_NAME HTTP_ADDR=127.0.0.1:8080 ./uptime-control
```

`DB_HOST` и `DB_NAME` нужно заменить настройками сервера. Пароль безопаснее передавать через менеджер секретов или защищённый файл окружения, а не записывать в командную историю.

Это пока тестовый запуск. Перед публичным production-развёртыванием ещё нужны reverse proxy, TLS, отдельный системный пользователь, сервисный менеджер, firewall и резервные копии.
