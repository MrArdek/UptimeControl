# Установка на тестовый Linux-сервер

Дата актуализации: 2026-09-06

Инструкция предназначена для первого закрытого тестового сервера. Для публичного production-запуска дополнительно нужны TLS, firewall, резервные копии и системный мониторинг.

## Рекомендуемая схема независимого мониторинга

Uptime Control следует устанавливать на отдельный сервер, который не является контролируемым проектом и не использует его PostgreSQL.

Рекомендуемый вариант:

```text
uptime.igra.ru ──DNS──> отдельный monitoring VPS ──HTTP/heartbeat──> основной сервер igra.ru
```

Если адрес `igra.ru/uptimec` проксируется через Nginx на основном сервере, при полном падении этого сервера Dashboard тоже станет недоступен. Подадрес поддерживается, но для независимости лучше отдельный поддомен с DNS, направленным непосредственно на monitoring VPS.

## Быстрая установка через Docker Compose

На отдельном сервере с Docker и Compose:

```bash
git clone https://github.com/MrArdek/UptimeControl.git
cd UptimeControl
cp deploy/compose.env.example .env
```

Замените `POSTGRES_PASSWORD` и `PUBLIC_ORIGIN` в `.env`, затем запустите:

```bash
docker compose up -d --build
docker compose ps
curl --fail http://127.0.0.1:8080/health
```

PostgreSQL не публикуется наружу, приложение слушает только loopback сервера, а данные БД сохраняются в Docker volume `uptime_control_postgres`. Публичный доступ должен проходить через HTTPS reverse proxy.

## 1. Требования

- Linux-сервер с отдельным непривилегированным пользователем приложения.
- Go 1.27 или новее в пределах совместимости проекта.
- PostgreSQL версии 14–18.
- Git.
- Домен и HTTPS, если авторизация проверяется через браузер.

## 2. Получение проекта

```bash
git clone https://github.com/MrArdek/UptimeControl.git
cd UptimeControl
```

При обновлении уже установленного проекта:

```bash
git pull --ff-only
```

## 3. Сборка

```bash
go mod download
go test ./...
go build -trimpath -o uptime-control .
```

Если тесты или сборка завершились ошибкой, старую рабочую версию сервера заменять нельзя.

## 4. PostgreSQL

Создайте отдельные базу и пользователя PostgreSQL. Не используйте администратора базы для постоянной работы приложения.

Не записывайте реальный пароль в репозиторий или историю команд. Сохраните строку подключения в защищённом файле окружения либо менеджере секретов.

На текущем этапе пользователь приложения должен иметь право создавать и изменять таблицы, потому что миграции выполняются при старте.

## 5. Переменные окружения

Обязательная production-конфигурация:

```text
HTTP_ADDR=127.0.0.1:8080
DATABASE_URL=<секретная строка подключения PostgreSQL>
PUBLIC_ORIGIN=https://monitor.example.com
BASE_PATH=
SESSION_COOKIE_SECURE=true
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=
```

- `HTTP_ADDR` лучше оставлять на loopback и открывать приложение через reverse proxy.
- `PUBLIC_ORIGIN` должен точно совпадать с адресом в браузере и не содержать путь.
- `BASE_PATH` оставьте пустым для отдельного домена либо задайте, например, `/uptimec` для адреса `https://igra.ru/uptimec`.
- `SESSION_COOKIE_SECURE=true` требует HTTPS и обязателен для публичного сервера.
- `TELEGRAM_BOT_TOKEN` и `TELEGRAM_CHAT_ID` задаются вместе для уведомлений либо остаются пустыми.

Пример для установки по адресу `https://igra.ru/uptimec`:

```text
HTTP_ADDR=127.0.0.1:8080
DATABASE_URL=<секретная строка подключения PostgreSQL>
PUBLIC_ORIGIN=https://igra.ru
BASE_PATH=/uptimec
SESSION_COOKIE_SECURE=true
```

Защитите файл окружения правами чтения только для системного пользователя приложения.

## 6. Первый запуск

Перед созданием системного сервиса можно выполнить закрытую проверку вручную:

```bash
./uptime-control
```

Приложение должно сообщить о подключении к базе, применении миграций и запуске HTTP-сервера.

В другом терминале сервера:

```bash
curl --fail http://127.0.0.1:8080${BASE_PATH}/health
curl --fail http://127.0.0.1:8080${BASE_PATH}/ready
```

Оба запроса должны завершиться успешно.

## 7. Постоянный запуск

Для постоянной работы настройте systemd или другой сервисный менеджер:

- запуск от отдельного пользователя без shell-доступа;
- загрузка переменных из защищённого файла;
- рабочая папка проекта;
- автоматический перезапуск только при ошибке;
- корректная передача `SIGTERM`;
- запуск после PostgreSQL и сети.

Не запускайте приложение постоянно через `nohup`, `screen` или от пользователя `root`.

## 8. Reverse proxy и HTTPS

Настройте Nginx, Caddy или другой reverse proxy:

- принимайте публичные запросы только по HTTPS;
- перенаправляйте HTTP на HTTPS;
- передавайте запросы на `127.0.0.1:8080`;
- добавьте ограничение частоты запросов на `/api/v1/auth/login` и `/api/v1/auth/register`;
- не публикуйте PostgreSQL-порт в интернет.

Для Nginx и `BASE_PATH=/uptimec` минимальная схема маршрутизации выглядит так:

```nginx
location = /uptimec {
    return 308 /uptimec/;
}

location ^~ /uptimec/api/v1/heartbeat/ {
    access_log off;
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}

location /uptimec/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

У `proxy_pass` в этом примере нет завершающего `/`: Nginx передаёт приложению полный путь `/uptimec/...`, который ожидается при заданном `BASE_PATH`.

Для heartbeat location отключён access log, потому что секретный токен находится в URL. Общие журналы Nginx не должны сохранять этот путь целиком.

Текущая версия приложения не доверяет `X-Forwarded-For`, поэтому встроенный limiter за reverse proxy будет видеть адрес прокси. Основное production-ограничение частоты нужно настроить на самом reverse proxy.

## 9. Проверка после установки

Выполните проверки из `docs/VERIFICATION.md`. Дополнительно проверьте в браузере:

1. Регистрация создаёт cookie с `Secure`, `HttpOnly`, `SameSite=Lax` и путём из `BASE_PATH`.
2. `${BASE_PATH}/api/v1/auth/me` возвращает текущего пользователя.
3. После logout старая cookie больше не даёт доступ.
4. HTTP перенаправляется на HTTPS.
5. PostgreSQL недоступен из интернета.

## 10. Перед production

- Убедиться, что после регистрации первого владельца повторная регистрация возвращает `403 registration_closed`.
- Настроить ежедневный зашифрованный бэкап и тест восстановления.
- Добавить email-подтверждение и восстановление пароля.
- Определить централизованный rate limit для нескольких реплик.
- Настроить сбор логов без cookie, токенов и паролей.
- Проверить обновление без потери пользовательских данных.
