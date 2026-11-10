# Внутренние Go-интерфейсы

Решение принято Лёшей 9 ноября 2026 по переданным командой полномочиям. Это рабочий контракт реализации #9–#14. Независимая проверка подключения модулей еще впереди. HTTP/SSE v0.2 сохранен. [План соединения](architecture.md), [словарь](../../CONTEXT.md), [типы Go](../../backend/internal/ports/ports.go).

## Владение

| Владелец | Область |
|---|---|
| Лёша | app/platform, HTTP сессий/плана/чата/заявок, их SQL, drafts/request-service, worker, миграции, Compose/CI |
| Рома | agent, knowledge, LLM/embedding-клиенты, промпты/tools, документы и их SQL/ACL, обработчик GET document, docs/sdd |
| Егор | integrations, схема demo_external, адаптеры задачи/доступа, команда demo-control |

Один module в backend. app собирает зависимости. Agent не импортирует HTTP/app/SQL Store. Общие типы находятся в ports. Изменения общих интерфейсов ведет Лёша с обновлением потребителей в том же изменении.

## Доверенный контекст

Все вызовы принимают context.Context. Session middleware устанавливает Principal(UserID, TeamID, Role); инструменты модели не принимают эти поля. Для worker контекст восстанавливается из сохраненного владельца операции. Модули отдельно проверяют доступ к своим объектам. Чужой объект скрывается ErrNotFound. В тестах используется testkit.Context.

## Вызовы

Все перечисленные методы возвращают также error; точные типы закреплены в ports.go.

| Порт | Методы |
|---|---|
| Agent | Run(ctx, AgentInput, Emit) → AgentResult |
| Knowledge | Search(ctx, query) → []Source; Read(ctx, documentID) → Document |
| TaskProvider | FirstTask(ctx) → Task |
| AccessChecker | Check(ctx, AccessCheck) → bool |
| AccessProvisioner | Submit(ctx, AccessSubmission) → AccessResult; Lookup(ctx, requestID) → AccessResult |
| AccessProvider | Объединяет AccessChecker и AccessProvisioner для реализации Егора |
| RequestReader | Get(ctx, requestID) → RequestState; FindActive(ctx, AccessCheck) → RequestState |
| DraftService | SaveAndLink(ctx, DraftInput) → Draft |

Агенту передается только **AccessChecker**, worker получает AccessProvisioner. Submit отсутствует в зависимостях tools. RequestReader и DraftService реализует Лёша.

Search ограничивает query 2000 code points, возвращает до 5 доступных фрагментов по 1200 code points. Read возвращает id/title/content/updated_at, документ до 16000 code points. Лимиты находятся внутри knowledge, поля limit в порте нет. FirstTask выбирает первую задачу в устойчивом порядке пользователя/команды; для MVP достаточно одной. Это выбранное сужение ListMine из #13, не новый HTTP endpoint.

## Run и события

AgentInput содержит проверенные IDs диалога/assistant/шага, вопрос, историю без повторного включения текущего вопроса и Requests — актуальные связанные заявки. Run синхронный; emit вызывается последовательно и не используется после возврата. Ошибка emit/отмена ctx останавливает цикл. AgentResult.Sources и RequestIDs возвращаются и при ошибке после частичной работы.

| Событие | Поле | Действие chat service |
|---|---|---|
| answer.delta | Text | Накапливает/сохраняет текст, передает SSE |
| tool.started | Tool{CallID, Label} | Передает разрешенную подпись |
| tool.finished | Tool{CallID, Status} | Передает succeeded/failed |
| draft.saved | Draft после commit | Читает актуальную заявку, отправляет action.proposed только для draft |

message.started/completed/failed формирует chat service Лёши. Он сохраняет полный итог до terminal event; не раскрывает сырые LLM payload или рассуждения. Sources берутся из действительно полученных материалов; RequestIDs проверяются по владельцу.

Изменение относительно первого каркаса: draft.proposed с DraftInput заменен на draft.saved с сохраненным Draft. Инструмент Ромы вызывает DraftService и получает фактические ID/версию до следующего вызова LLM. Emit возвращает только error, HTTP callback не возвращает результат команды.

## prepare_access_request

1. Проверить типизированные аргументы resource_id/permission/reason. Имя ресурса получить из доверенного каталога задачи.
2. Check=true означает уже выданный доступ — новая заявка не нужна. FindActive ищет draft/submitting/pending для пользователя/ресурса/права. Для submitting/pending вернуть статус без draft.saved.
3. IDs conversation/assistant/step привязать из AgentInput. Новый RequestID генерирует Go; для уточнения используется существующий draft ID. Модель не задает эти поля.
4. Вызвать SaveAndLink, получить Draft с датами/версиями, добавить ID в AgentResult.RequestIDs, затем emit(draft.saved).

DraftService — команда модуля Лёши. Агент вызывает ее, но не пишет SQL и не управляет транзакцией. Координатор уже есть в internal/drafts; SQL Store появится с #14. Store.WithinTx использует database.WithinTx и один *sql.Tx для UpsertDraft и LinkAssistant. Перед транзакцией request-service проверяет ресурс через доверенный каталог/порт; сетевых вызовов внутри SQL-транзакции нет. Внутри проверяются владелец, состояния, согласованность диалога/assistant/шага, версии и уникальность активного доступа. Реальная проверка каталога и SQL Store поставляются в #14, текущий координатор проверяет форму полей и связывает записи через Store.

Для draft текущего диалога разрешено уточнение. Если активная заявка создана из другого диалога/шага, исходные связи не переписываются: инструмент возвращает существующую запись через RequestReader. Ее ID можно включить в итог после проверки владельца. Это также восстановление после ErrConflict между FindActive и SaveAndLink.

Version соответствует HTTP draft_version; Revision — revision. Даты задает БД в UTC. Повтор без изменения параметров не увеличивает версии; изменение параметров увеличивает обе. Редактирование не-draft запрещено. Новый draft после failed/rejected — только по явному новому запросу. Добавление связи с очередным сообщением само по себе не меняет параметры заявки.

Ошибка LinkAssistant откатывает UpsertDraft. Commit до ошибки emit остается в истории. Callback чата загружает сохраненную запись и проверяет владельца/привязку, не доверяет произвольному Draft из события. Если состояние уже не draft, action.proposed не отправляется; финал/GET восстановит актуальную карточку через RequestIDs.

## Отправка и ошибки

Submit принимает постоянный RequestID. Повтор точных параметров возвращает прежний ExternalID; другой payload под тем же ключом — ErrConflict. Lookup ищет по RequestID с проверкой владельца и возвращает актуальное решение.

ErrOutcomeUnknown означает «могла создаться»: остается submitting, идет Lookup с прежним ключом. ErrNotFound от Lookup — достоверное отсутствие на момент проверки, не сетевой сбой; Submit повторяется только с тем же ключом/payload. Имитация обязана сохранить идемпотентность даже при запоздавшем первом Submit. ErrUnavailable не означает failed.

AccessResult использует pending/granted/rejected/failed. Для первых трех обязателен ExternalID, для rejected/failed — безопасная FailureReason. Достоверный отказ до записи — failed без ExternalID. granted/rejected — конечные решения.

Общие ошибки поддерживают errors.Is: identity.ErrUnauthenticated, ports.ErrNotFound/ErrForbidden/ErrInvalid/ErrConflict/ErrUnavailable/ErrOutcomeUnknown и ошибки context. Модули не отправляют err.Error модели/браузеру. HTTP-код выбирается по операции и behavior.md.

## Тестовые подключения

testkit.Agent, Knowledge, Tasks, Access, Requests и Drafts доступны для независимой работы. Это doubles, не долговечное хранилище/LLM. Согласованный интерфейс не доказывает реальную интеграцию. Задания: [Рома](roma.md), [Егор](egor.md).

## Сессии и план (#9)

`auth.Handler.Require` проверяет cookie и устанавливает доверенный Principal.
Для бизнес-маршрутов передается internOnly=true; сервисы повторно проверяют роль
и владельца. `app.NewHandler` получает ports.AccessChecker для правил завершения
шага; в DEMO_MODE до #13 явно подключается demo.Access без выданных прав.

`onboarding.LockPlanTx(ctx, tx, owner)` задает порядок блокировок для #14:
план, затем запись заявки/job. `ApplyRequestStateTx(ctx, tx, owner, stepID, request)`
обновляет связь и блокировку шага в той же транзакции. request — ports.RequestState;
валидность версии/перехода заявки и права проверяет request-service. После изменения
revision плана старый PATCH повторяет проверку. SQL-транзакция не содержит сетевых
вызовов. Подробности и примеры: [сессии и план](session-onboarding.md).

## Чат (#10)

Composition root получает app.Dependencies с Access, Agent и Requests. Сигнатуры
ports.Agent/RequestReader/DraftService сохранены. Chat хранит сообщения/историю,
управляет SSE и отменой. В DEMO_MODE явно подключён demo.Agent без источников/заявок;
вне demo отсутствие Agent даёт 503. [Запуск и подключение](chat.md).

`chat.LinkRequestTx(ctx, tx, owner, assistantID, requestID)` — SQL-стык для #14:
вызывается в транзакции сохранения проверенного draft. Порядок блокировок:
план → заявка → диалог → assistant. Helper не коммитит и не подтверждает заявку.
draft.saved перечитывается через RequestReader, сверяется с SQL-связью и контекстом;
только актуальный draft превращается в action.proposed. Финал сохраняет committed
связи даже при ошибке модели. Настоящий SQL Store заявок/confirm остаются в #14.
