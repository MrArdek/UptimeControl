# Текст GitHub issue: frontend stack этапа 11

Заголовок:

```text
Approve React and Vite dependencies for the stage 11 frontend
```

Тело issue:

```markdown
## Назначение

Этап 11 требует заменить встроенный HTML Dashboard на полноценный TypeScript + React интерфейс: вход, проекты, monitors, история, инциденты, настройки, уведомления, общие таблицы/графики и живое состояние через SSE.

## Предлагаемое решение

Статический React SPA собирается Vite и встраивается в Go binary. Node.js нужен только для разработки/build; production остаётся одним Go-процессом. Vite `base: "./"`, HTML `<base>` и runtime-конфигурация от Go сохраняют один артефакт для `/` и произвольного `BASE_PATH`.

Зависимости по состоянию на 2026-09-14:

- runtime: `react@19.3.0`, `react-dom@19.3.0`;
- development: `typescript@7.0.2`, `vite@8.3.0`, `@vitejs/plugin-react@6.1.1`, `@types/react@19.3.0`, `@types/react-dom@19.3.0`.

Версии будут зафиксированы в `package-lock.json`; production build выполняется через `npm ci && npm run build`.

## Альтернативы

1. Сохранить vanilla JS: нет новых зависимостей, но сложнее безопасно развивать множество экранов, общие состояния и типизированные API-контракты.
2. Next.js static export: статика встраивается в Go, но `basePath` зашивается при build и мешает одному бинарнику работать с runtime `BASE_PATH`; серверные Next-функции в export недоступны.
3. Next.js runtime: даёт SSR, но добавляет второй production-процесс и proxy/deploy surface. SSR не нужен для приватного live Dashboard.

## Лицензии и размер

- React, ReactDOM, Vite, React plugin и type packages: MIT.
- TypeScript: Apache-2.0.
- Суммарный unpacked size прямых пакетов npm — около 13,7 МБ; это не размер браузерного bundle. После первой сборки в issue будет добавлен фактический minified/gzip размер.
- Node.js и dev dependencies не входят в production runtime. Скомпилированные assets входят в Go binary.

## Обновления и безопасность

- npm supply-chain риск ограничивается lockfile, `npm ci`, review изменений lockfile и audit в CI.
- Никаких CDN и внешних frontend runtime.
- CSP будет запрещать inline scripts; строки пользователя рендерятся React без raw HTML.
- Дополнительные router/chart/state/CSS зависимости не добавляются этим решением.
- Major-обновления требуют отдельного review; уязвимости проверяются перед релизом.

## Критерии проверки

- build воспроизводим через lockfile;
- один артефакт работает на `/` и `/uptimec`;
- refresh вложенного маршрута возвращает приложение;
- logout закрывает SSE и отозванная сессия больше не получает снимки;
- fallback polling и индикатор устаревших данных проверены тестами;
- фактический размер production bundle записан в документацию.
```
