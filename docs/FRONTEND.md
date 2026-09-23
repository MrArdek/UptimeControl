# Frontend: архитектурное решение этапа 11

Дата решения: 2026-09-14

Статус: **реализовано 2026-09-23 по явному разрешению владельца работать без GitHub issue**.

## Требования проекта

- TypeScript + React для полноценного Dashboard.
- Один self-hosted артефакт без Node.js в production.
- Одна сборка должна работать при пустом `BASE_PATH` и под произвольным префиксом, например `/uptimec`.
- Вход, проекты, monitors, история, инциденты, настройки и уведомления используют существующий Go API и cookie-сессии.
- Вложенные frontend-маршруты должны открываться и после обновления страницы.
- Живые состояния приходят через существующий `GET /api/v1/events`, при недоступности SSE используется polling.
- Никаких frontend-файлов с CDN: скрипты и стили поставляются вместе с Go-приложением.

## Рассмотренные варианты

| Вариант | `BASE_PATH` | Установка и поставка | Размер и runtime | SSR |
| --- | --- | --- | --- | --- |
| Next.js с отдельным runtime | Можно настроить, но нужен согласованный proxy и отдельный frontend-процесс | Два production-процесса, два health check и отдельное обновление | Самый большой эксплуатационный объём; Node.js обязателен в production | Есть, но приватный Dashboard всё равно получает живые данные после входа |
| Next.js static export | `basePath` встраивается в клиентские bundles при сборке, поэтому смена префикса требует новой сборки | Статика может раздаваться Go, но часть Next-функций недоступна | Node.js только при build; Next остаётся дополнительным слоем сборки | В runtime отсутствует |
| React + TypeScript + Vite | `base: "./"` предназначен для embedded deployment; Go подставляет фактический базовый URL в HTML и runtime-конфигурацию | Node.js только для воспроизводимой сборки; готовые assets встраиваются в Go | Меньше обязательных пакетов и один Go-процесс в production | Не нужен |

## Решение

Выбрать **React + TypeScript + Vite**, собрать одностраничное приложение в статические assets и встроить результат в Go через `embed.FS`.

Основания:

- Uptime Control уже имеет полноценный Go backend, серверные cookie-сессии и API. Второй серверный framework не добавляет нужной функции.
- Данные Dashboard приватны и быстро меняются. SSR не устраняет запрос после авторизации и не заменяет SSE.
- Vite официально поддерживает пустой или относительный `base` для embedded deployment. Это позволяет сохранить один build, а Go сможет задавать `BASE_PATH` при запуске.
- React рекомендует Vite как один из вариантов сборки приложения с нуля.

Официальные материалы:

- [React: Build a React app from Scratch](https://react.dev/learn/build-a-react-app-from-scratch)
- [Vite: Shared Options — base](https://vite.dev/config/shared-options.html#base)
- [Vite: Deploying a Static Site](https://vite.dev/guide/static-deploy.html)
- [Next.js: Static Exports](https://nextjs.org/docs/app/guides/static-exports)
- [Next.js: basePath](https://nextjs.org/docs/pages/api-reference/config/next-config-js/basePath)

## Предлагаемые зависимости

Версии и сведения npm registry на 2026-09-14:

| Пакет | Роль | Версия | Лицензия | Unpacked size |
| --- | --- | --- | --- | --- |
| `react` | Компоненты и состояние UI | 19.3.0 | MIT | 179 КБ |
| `react-dom` | Рендеринг в браузере | 19.3.0 | MIT | 8,1 МБ |
| `typescript` | Проверка типов и компиляция TS/TSX | 7.0.2 | Apache-2.0 | 2,5 МБ |
| `vite` | Dev server и production build | 8.3.0 | MIT | 2,4 МБ |
| `@vitejs/plugin-react` | JSX transform и React Fast Refresh | 6.1.1 | MIT | 45 КБ |
| `@types/react` | TypeScript-типы React | 19.3.0 | MIT | 408 КБ |
| `@types/react-dom` | TypeScript-типы React DOM | 19.3.0 | MIT | 33 КБ |

`react` и `react-dom` попадают в браузерный bundle. Остальные пакеты нужны только при разработке и сборке. Фактический размер minified production assets фиксируется после первой разрешённой сборки; размер npm-пакета не равен размеру bundle.

Дополнительные router, chart, CSS и state-management библиотеки пока не нужны. Маршрутизация строится на History API, графики — на SVG, запросы — на `fetch` и `EventSource`.

## Реализованная структура

- `web/src/` — TypeScript/React исходники.
- `web/src/api/` — типизированный клиент существующего `/api/v1`.
- `web/src/components/` — таблицы, графики, пагинация и общие состояния.
- `web/src/pages/` — вход, проекты, monitor, уведомления и будущие разделы.
- `web/dist/` — воспроизводимый результат `npm run build`, встраиваемый в Go.
- `internal/httpserver/dashboard.go` — раздача assets и SPA fallback только внутри `BASE_PATH`.

Vite собирается с относительным `base`. Go отдаёт HTML с `<base href="{BASE_PATH}/">` и runtime-значением базового пути. Frontend строит API/SSE URL только из этого значения. Сервер возвращает `index.html` для известных вложенных frontend-маршрутов, но не маскирует отсутствующие `/api/` и asset-файлы.

## Риски и меры

- **npm supply chain:** версии фиксируются lockfile; CI использует `npm ci`; перед обновлением запускаются `npm audit` и review lockfile.
- **XSS:** пользовательские строки выводятся через React escaping; `dangerouslySetInnerHTML` запрещён; CSP переводится на внешние hashed assets без `unsafe-inline` для scripts.
- **Устаревшие пакеты:** Dependabot/плановая проверка обновлений добавляются на этапе CI, major-обновления проходят отдельную проверку.
- **Слишком большой bundle:** первая сборка фиксирует gzip-размер; для страниц применяется lazy loading, если основной bundle превысит согласованный бюджет.
- **Ошибки маршрутизации:** автоматические HTTP-тесты проверяют `/`, `/uptimec/`, вложенный route, assets, API 404 и runtime `BASE_PATH`.
- **Потеря live-состояния:** UI показывает время последнего снимка, переподключается к SSE и включает polling с явным признаком устаревших данных.

## Разрешение

Владелец явно разрешил перечисленные зависимости и 2026-09-23 отдельно поручил продолжить без GitHub issue. `FRONTEND_DEPENDENCY_ISSUE.md` сохранён как журнал выполненной оценки, но больше не блокирует сборку.
