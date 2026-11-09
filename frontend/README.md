# FE1: API и тестовая основа (#2)

Node 24.15.x, npm (версии пакетов закреплены в package-lock.json).
Override minimatch 11.0.1 устраняет GHSA-g3hv-75wg-m249 в зависимостях линтера;
совместимость проверяется lint и тестами.

```sh
npm ci
npm run generate:api
npm run check:api
npm run typecheck
npm run lint
npm run test:coverage
```

## Запуск объединённого фронтенда

Из папки frontend:

```sh
npm ci
npm run dev
```

Интерфейс открывается на http://localhost:5173. Vite проксирует /api на
http://localhost:8080; порт 5173 согласован с ALLOWED_ORIGINS бэкенда.

Объединены UI-каркас FE2 и API-инфраструктура FE1. Экраны login/onboarding/chat
пока демонстрационные: данные пользователя и плана статические, ответ чата
формируется таймером, номер ACCESS-1001 не подтверждает реальную отправку.
Настоящие session/plan/requests, SSE и источники подключаются в #3–#6.

Проверки: npm run check:api, npm run typecheck, npm run lint,
npm run test:coverage, npm run build. CI запускает все эти команды.
Общий QueryClient создаётся через createQueryClient; строгие настройки TypeScript,
алиас @/*, MSW setup и пороги покрытия объединены без отключения проверок.

## Публичный API для FE2

Импорт из `src/shared/api/index.ts`: apiRequest, openApiStream, ClientError,
apiErrorSchema, queryKeys, createQueryClient и алиасы моделей OpenAPI.

```ts
// В feature session: userSchema — Zod-схема ответа, которой владеет FE2.
const result = await apiRequest('/me', userSchema, { signal });
// result.status сохраняется; result.data === undefined для 204.

// В feature access-requests: accessRequestSchema — публичная схема FE2.
const result = await apiRequest(
  `/access-requests/${encodeURIComponent(requestId)}/confirm`,
  accessRequestSchema,
  { method: 'POST', body: { draft_version: displayedVersion }, signal },
);
// 202 не означает выдачу доступа. Дальше — GET и polling по контракту.
// Для DELETE /session можно передать z.never(): ответ 204 не разбирается.
```

У входящего JSON тип unknown до проверки схемой. Схемы HTTP принадлежат features,
сгенерированный TypeScript не выполняет runtime-проверку. Файл errors.ts содержит
только общую схему ошибки. Не редактировать generated/schema.ts вручную.
CI и check:api проверяют, что повтор генерации не меняет файл.

`ClientError.kind`: http/network/protocol/aborted. Сохраняются status, code,
traceId, fieldErrors и Retry-After (строка; секунды или HTTP-date). HTML прокси
не выводится пользователю. Отмена не должна выглядеть как ошибка отправки.
Транспорт не повторяет запросы. QueryClient разрешает максимум два повтора GET
при network/5xx, исключает 401, ошибки схемы и отмену; мутации retry=false.
Не менять retry для confirm без сверки состояния по контракту.

FE2 создаёт ровно один createQueryClient() в providers; не создавайте его при каждом
рендере. При выходе/401 FE2 отменяет queries и очищает кеш, а будущий контроллер
потока FE1 отменяется через переданный AbortSignal. Персистентного кеша нет.
openApiStream только открывает проверенный SSE-ответ; reader/recovery — задача #3.

## MSW и тесты

`createHandlers(fe2Handlers)` из mocks/handlers объединяет handlers двух авторов.
FE2 передаёт session/plan/requests; FE1 поставляет chat/documents. Реализация FE2
не подменяется фиктивной бизнес-логикой в shared/api. Пример GET/confirm с
сохранением состояния приведён в mocks/handlers/handlers.test.tsx.

Базовые данные клонируются из contracts/examples/scenario.json. schemas.ts внутри
mocks/fixtures содержит только тестовую проверку контракта, а не production-схемы
модулей FE2. При появлении feature-схем применять их к тем же fixtures в тестах.
История и источники доступны на GET. Создание диалога идемпотентно по UUID.
Streaming POST/восстановление, полный автомат заявки и SSE-сценарии — #3/#5/#7.

Тестовый server использует onUnhandledRequest:error, сбрасывает handlers и состояние
после каждого теста. renderWithQuery создаёт отдельный QueryClient без повторов
и очищает его после теста. Для component-тестов доступен RTL и jest-dom.

Worker уже находится в public/mockServiceWorker.js. Для явного включения
создайте frontend/.env.local со строкой VITE_API_MOCKING=enabled и запустите npm run dev.
main.tsx дожидается запуска MSW перед React только в dev-режиме.
По умолчанию и в production MSW не запускается; ошибка Go не маскируется.

Пока подключены handlers chat/documents. Session/plan/requests добавит FE2;
отсутствующий handler считается ошибкой. Визуальные заглушки пока не обращаются
к этому API автоматически. После обновления MSW worker обновляется командой
npm exec -- msw init public.

Browser-моки держат состояние только в памяти: они не подтверждают сохранение при
reload. Для такого e2e в #7 нужен серверный mock/backend с сохранением состояния.

Покрытие: весь src, исключены generated, тесты и тестовая инфраструктура/mocks.
Порог lines 81%; отчёты coverage/index.html и coverage/lcov.info.

## Интеграция и визуальный контекст

Рабочая интеграционная ветка — dev. Новые task-ветки создаются от dev и объединяются
в dev; main предназначена для согласованной готовой версии.

В #2 нет экранов. Общие стили и UI принадлежат FE2. Предоставленные пользователем
референсы светлого/тёмного мобильного чата Авито применяются в #3/#6: читаемая
типографика, спокойные поверхности, отчётливый composer и состояния ожидания.
ui-ux-pro-max используется как справочник качества; токены и компоненты #1 имеют
приоритет над предложениями генератора палитр. Тёмная тема требует согласованных
общих токенов FE2, а не локальной перекраски чата.
