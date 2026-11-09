# Внутренние Go-интерфейсы: передача первого дня

Статус: **предложены для согласования с BE2 и интеграциями**, а не согласованы за них.
HTTP/SSE-контракт v0.2 сохранен. Все вызовы внутри одного Go-процесса.
Код интерфейсов: [ports.go](../../backend/internal/ports/ports.go).
Типы внутренних портов не являются JSON DTO; преобразование и проверка wire-схем — BE1.

## Владение и зависимости

| Участник | Владеет | Получает / передает |
|---|---|---|
| BE1, Лёша | cmd, app, platform, HTTP, чат/план/сессии/заявки и SQL persistence | Инъекция портов в app, сохранение данных до события |
| BE2, Рома | Новый `internal/agent`, `internal/knowledge` | Реализует Agent, Knowledge; использует TaskProvider/AccessProvider.Check и предложения черновика |
| Интеграции, Егор | Новый `internal/integrations` | Реализует TaskProvider и AccessProvider, ключом внешней операции служит request_id |
| BE1 + потребители | `internal/ports`, этот файл | Изменения подписей согласуются вместе |

Новые Go-пакеты создаются внутри **backend/**: `internal` намеренно недоступен
другому module. Не создавайте второй go.mod. Общие миграции нумерует BE1 перед merge.
`ports` не импортирует app, HTTP, agent, integrations или persistence; циклов нет.

## Доверенный контекст

Все операции принимают `context.Context`. `identity.Require(ctx)` возвращает
Principal с UserID, TeamID, Role. Заполнять его разрешено session middleware после
проверки серверной сессии; сегодня middleware сессий еще не реализован (#9).
В тестах используется `testkit.Context(context.Background())`.
UserID/TeamID/Role отсутствуют в аргументах инструментов модели. Права на каждый
ресурс, документ, шаг и объект проверяются отдельно. Один Principal не доказывает
доступ к произвольному ID. Для чужого объекта возвращается ErrNotFound.

В фоновой отправке доверенный Principal восстанавливается из сохраненного владельца
операции, а не из нового браузерного запроса или текста модели.

## Вызовы

| Порт | Подписи и обязательства |
|---|---|
| Agent | `Run(ctx, AgentInput, Emit) (AgentResult, error)`; не пишет HTTP/SQL; вызывает emit последовательно, прекращает при ошибке callback/отмене, не сохраняет callback после возврата |
| Knowledge | `Search(ctx, query) ([]Source, error)` и `Read(ctx, documentID) (Document, error)`; фильтрует права до возврата материалов, пустой поиск — обычный результат |
| TaskProvider | `FirstTask(ctx) (Task, error)`; задача для доверенного пользователя/команды |
| AccessProvider | `Check(ctx, AccessCheck) (bool, error)`, `Submit(ctx, AccessSubmission) (AccessResult, error)`, `Lookup(ctx, requestID) (AccessResult, error)` |
| DraftService | `SaveAndLink(ctx, DraftInput) (Draft, error)`; атомарное сохранение черновика и связи с assistant, без внешней отправки |

AgentInput содержит доверенные ConversationID, AssistantMessageID, StepID, вопрос
и историю. HTTP/chat service проверяет принадлежность объектов до Run.
AgentResult возвращает источники; текст собирается вызывающим из `answer.delta`.

Внутренние события AgentEvent:

- `answer.delta`: поле Text;
- `tool.started`: Tool{CallID, Label};
- `tool.finished`: Tool{CallID, Status: succeeded/failed};
- `draft.proposed`: DraftInput; это предложение к сохранению, еще не SSE action.proposed.

Вызывающий проверяет kind/payload и привязку к текущему диалогу/assistant; Рома не
формирует `message.started/completed/failed`. Ими владеет chat service BE1.
В wire-события не включаются внутренние рассуждения, технические ошибки или секреты.
До полного UI/чата fake Agent можно вызвать через компилируемый
[пример](../../backend/internal/ports/example_test.go).

## Сохранение черновика без циклических импортов

Координатор уже есть: [drafts.Service](../../backend/internal/drafts/service.go).
Потребитель вызывает SaveAndLink; реализация Store выполняет одну транзакцию:

```go
// Реализация SQL Store появляется с таблицами чата/заявок.
return database.WithinTx(ctx, db, func(tx *sql.Tx) error {
    return fn(newDraftTx(tx)) // один tx для UpsertDraft и LinkAssistant
})
```

`newDraftTx` выше — иллюстрация будущего адаптера, а не уже существующая функция.
`drafts.Tx.UpsertDraft` проверяет владельца conversation/assistant/step и доступ к
ресурсу, блокирует запись draft, проверяет состояние и версии. Изменение параметров
увеличивает draft_version и revision; идемпотентный повтор не меняет версии.
Уже подтвержденную заявку редактировать нельзя. LinkAssistant проверяет владельца,
роль assistant, совпадение conversation и не добавляет дубликат request_id.
Все проверки и обе записи выполняются через тот же sql.Tx.

Только после успешного commit callback emit возвращает сохраненную заявку в
SSE `action.proposed`. При ошибке связи черновик откатывается; при сбое доставки
после commit история восстанавливает связь. Это проверяется unit-тестом
координатора; настоящего SQL Store заявок пока нет.

## Внешняя операция и ошибки

Модель может вызывать Check, но **Submit ей не передается**. Submit доступен только
worker после явного confirm, сохраненного вместе с задачей отправки. До #14 этого
worker/confirm нет. Текст «подтверждаю» не дает право отправки.

AccessSubmission.RequestID — постоянный ключ идемпотентности. Повтор тех же параметров
возвращает тот же результат, изменение под тем же ключом — ErrConflict.
Lookup ищет по request_id. Неизвестный сетевой исход Submit — ErrOutcomeUnknown;
worker оставляет submitting и сверяет прежний ключ. При временной ошибке Lookup
нельзя объявлять, что внешней заявки нет. ErrNotFound допустим только для достоверного
отсутствия в имитации. pending/granted/rejected требуют ExternalID;
rejected/failed требуют безопасной FailureReason. У адаптера нет права менять план напрямую.

Общие errors.Is-совместимые ошибки: identity.ErrUnauthenticated, ports.ErrNotFound,
ErrForbidden, ErrInvalid, ErrConflict, ErrUnavailable, ErrOutcomeUnknown.
Оборачивание — `%w`; context.Canceled/DeadlineExceeded сохраняются. Их HTTP-коды
определяются конкретной операцией по behavior.md, не прямой отправкой err.Error клиенту.

## Заглушки и передача

`testkit.Agent` принимает Events/Result/Err; `testkit.Knowledge`, `Tasks` используют
общие синтетические ID; `testkit.Access` потокобезопасен и сохраняет один результат
на ключ в памяти; `testkit.Drafts` адаптирует тестовый callback. Это doubles для
разработки, не постоянное хранилище и не настоящий LLM. Номер FAKE-* маркирует имитацию.
Ни одна заглушка не включается в бизнес-HTTP как скрытый fallback.

Передача Роме: реализовать Agent/Knowledge; проверить пример Run и ошибку emit;
согласовать предложения draft и источник доверенной истории.
Передача Егору: реализовать TaskProvider/AccessProvider; подтвердить Submit+Lookup
с одним request_id и правила неопределенного исхода.
Передача фронтенду: порт 8080, dev proxy и формат ошибок в
[backend/README.md](../../backend/README.md). Вход/план появятся в #9.

Фактическое согласование потребителями остается открытым; этот документ не является
сообщением им и не подтверждает их согласие.
