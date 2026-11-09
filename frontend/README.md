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

Это основа одного будущего Vite-приложения, не второй React-проект.
На момент #2 каркас #1 отсутствовал в локальном main. Поэтому добавлен минимальный
package.json/tsconfig для воспроизводимых проверок. FE2 сохраняет владение этими
файлами и дополняет их router, Vite и scripts dev/build/test:e2e; второй lockfile не нужен.
Сборка приложения и e2e появятся с каркасом и экранами, в #2 они не заявляются.

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

Чтобы включить MSW в каркасе FE2, сначала сгенерируйте worker:

```sh
npm exec msw init public --save
```

Перед mount React, только при import.meta.env.DEV и явном VITE_API_MOCKING=enabled,
динамически импортируйте mocks/browser и await startBrowserMocks({
development: import.meta.env.DEV, enabled: import.meta.env.VITE_API_MOCKING === 'enabled',
handlers: fe2Handlers }). В обычной сборке моки не запускаются, сбой Go не маскируется.
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
