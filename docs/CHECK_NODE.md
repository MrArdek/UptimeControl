# Regional Check Node

Дата актуализации: 2026-09-23

Regional Check Node — отдельный Go-бинарник, который получает назначенные HTTP/TCP-проверки через Monitoring API и возвращает результаты. Узел не подключается к PostgreSQL и не получает данные других владельцев или неназначенных monitors.

## Регистрация

1. В Dashboard открыть «Серверы».
2. Создать узел с названием и кодом региона (`eu-central`, `asia-east`, `us-east` или своим кодом).
3. Сохранить `CHECK_NODE_SECRET`: исходный секрет показывается один раз, backend хранит только SHA-256-хеш.
4. Назначить узлу HTTP/TCP monitors. Heartbeat не выдаётся региональным узлам.

## Сборка и запуск

```bash
go build -trimpath -o check-node ./cmd/check-node
CHECK_NODE_BACKEND_URL=https://monitoring.example.com/uptimec \
CHECK_NODE_SECRET='секрет-из-dashboard' \
CHECK_NODE_REGION=eu-central \
./check-node
```

`CHECK_NODE_BACKEND_URL` включает `BASE_PATH`, если он задан, и в production обязан использовать HTTPS. HTTP разрешается только явным `CHECK_NODE_ALLOW_INSECURE_HTTP=true` для локальной проверки.

Переменные:

| Переменная | Назначение | По умолчанию |
| --- | --- | --- |
| `CHECK_NODE_BACKEND_URL` | Публичный HTTPS URL backend вместе с `BASE_PATH` | обязательна |
| `CHECK_NODE_SECRET` | Индивидуальный отзываемый bearer secret | обязательна |
| `CHECK_NODE_REGION` | Точный код региона из Dashboard | обязательна |
| `CHECK_NODE_HEALTH_ADDR` | Локальный health listener | `127.0.0.1:8090` |
| `CHECK_NODE_BUFFER_FILE` | Устойчивый локальный буфер результатов | `check-node-buffer.json` |
| `CHECK_NODE_POLL_INTERVAL` | Частота получения заданий и отправки буфера, 1–60 с | `5s` |
| `CHECK_NODE_ALLOW_INSECURE_HTTP` | Разрешить HTTP до backend для локального теста | `false` |

`GET http://127.0.0.1:8090/health` возвращает время запуска, последний успешный контакт с backend и размер буфера. Ответ не содержит секрет.

## systemd

```bash
sudo install -m 0755 check-node /opt/uptime-control/check-node
sudo install -d -m 0750 /etc/uptime-control
sudo install -m 0600 deploy/check-node.env.example /etc/uptime-control/check-node.env
sudo install -m 0644 deploy/uptime-check-node.service /etc/systemd/system/uptime-check-node.service
sudoedit /etc/uptime-control/check-node.env
sudo systemctl daemon-reload
sudo systemctl enable --now uptime-check-node
sudo systemctl status uptime-check-node
curl -i http://127.0.0.1:8090/health
```

Для Docker используется `Dockerfile.check-node`; файл буфера следует подключить к постоянному volume.

## Доставка, аренда и кворум

- Backend выдаёт только просроченные назначения и сразу переносит `next_check_at`; параллельные pollers используют `FOR UPDATE SKIP LOCKED`.
- Узел выполняет не больше четырёх проверок одновременно и применяет ту же DNS/IP SSRF-политику, таймауты и redirect-проверки, что основной backend.
- Результат получает UUID `result_id`; повтор `(node_id, result_id)` принимается без второй записи.
- До 1 000 результатов сохраняются в JSON-буфере с атомарной заменой файла и правами `0600`. После восстановления связи они отправляются batch до 100 элементов.
- Запоздалый результат хранится, но не откатывает более новое состояние monitor. События из будущего больше чем на 5 минут и старше 30 дней отклоняются.
- Для monitor с `N` активными узлами глобальный статус меняется только при согласии `floor(N/2)+1` свежих регионов. Меньшее число или разногласие означает отсутствие нового глобального решения; региональные наблюдения остаются в истории.
- Активное региональное назначение исключает monitor из локального scheduler. После удаления всех назначений backend снова выполняет локальную проверку.
- Отзыв узла немедленно прекращает bearer-доступ и выключает его назначения. Новый ключ создаётся только новой регистрацией.

## Развёртывание регионов

Одинаковый бинарник запускается без изменения кода в Европе, Азии, Северной Америке или произвольном регионе. Географию определяет фактический сервер, а не название региона. Для production-приёмки нужны действительно разнесённые серверы; несколько локальных процессов проверяют протокол и кворум, но не подтверждают географическое наблюдение.
