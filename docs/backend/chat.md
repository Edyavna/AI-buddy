# Чат, история и SSE — №10

Go реализует пять операций OpenAPI v0.2. Сессии и план берутся из №9, React
подключает Никита в №3. Настоящий агент подключается через существующий ports.Agent.
Demo-ответ явно синтетический, источников и внешних заявок в нём нет.

## Запуск и запросы

Из корня репозитория:

```powershell
docker compose up --build --wait api
docker compose --profile demo run --rm seed
```

Без Docker из backend с переменными окружения из `.env.example`: `go run ./cmd/migrate`,
`go run ./cmd/seed`, `go run ./cmd/server`. На Windows используйте curl.exe;
ниже синтаксис bash, JSON одинаковый:

```bash
curl -c /tmp/buddy-cookie -H 'Origin: http://localhost:8080' \
  -H 'Content-Type: application/json' -d '{"account_id":"intern_demo_1"}' \
  http://localhost:8080/api/v1/demo/session

curl -b /tmp/buddy-cookie -H 'Origin: http://localhost:8080' \
  -H 'Content-Type: application/json' \
  -d '{"client_conversation_id":"11111111-1111-4111-8111-111111111111","step_id":"step_local_setup"}' \
  http://localhost:8080/api/v1/conversations

# Подставьте id из ответа. Для нового вопроса нужен новый UUID.
curl -N -b /tmp/buddy-cookie -H 'Origin: http://localhost:8080' \
  -H 'Content-Type: application/json' -H 'Accept: text/event-stream' \
  -d '{"client_message_id":"22222222-2222-4222-8222-222222222222","text":"Как запустить проект?"}' \
  http://localhost:8080/api/v1/conversations/CONVERSATION_ID/messages

curl -b /tmp/buddy-cookie http://localhost:8080/api/v1/conversations
curl -b /tmp/buddy-cookie http://localhost:8080/api/v1/conversations/CONVERSATION_ID
curl -b /tmp/buddy-cookie http://localhost:8080/api/v1/conversations/CONVERSATION_ID/messages
```

Список диалогов: updated_at по убыванию, при равенстве — id. Первое создание —
201, точный повтор UUID/step_id — 200. Отсутствующий step_id эквивалентен null.
Название — заголовок своего шага либо «Новый диалог». Заблокированный шаг тоже
доступен для вопроса. GET ничего не создаёт, смена step_id диалога не предусмотрена.

Текст — до 8000 Unicode code points, непробельный; сохраняется без trim.
UUID нормализуется к нижней канонической записи. Сообщения упорядочены серверным
sequence, даже при одинаковых датах; assistant ссылается на user через
in_reply_to_message_id. Обязательные массивы — [], nullable-поля — null, даты — UTC.

| HTTP / code | Значение |
|---|---|
| 401 UNAUTHENTICATED | Нет действующей сессии |
| 403 ROLE_FORBIDDEN / ORIGIN_FORBIDDEN | Mentor или недопустимый Origin |
| 404 NOT_FOUND | Отсутствующий/чужой диалог либо шаг |
| 409 IDEMPOTENCY_CONFLICT | Прежний UUID с другим текстом либо step_id |
| 409 MESSAGE_ALREADY_EXISTS | Вопрос принят, получить историю |
| 409 GENERATION_IN_PROGRESS | Другой вопрос занят; новый не сохранён |
| 422 VALIDATION_ERROR | Формат, поля, UUID, пустой/слишком длинный текст |
| 503 SERVICE_UNAVAILABLE | Нет агента/хранилища; сверить историю перед повтором |

Порядок отправки: сессия/роль/владелец → валидация → UUID → занятость.
Повтор не запускает агента. Два вопроса не могут одновременно занять генерацию:
транзакция блокирует диалог, unique index защищает generating assistant в БД.
User и assistant сохраняются одной транзакцией до начала SSE.

## Поток, отмена и restart

Заголовки: Content-Type=text/event-stream, Cache-Control=no-cache, no-store,
X-Accel-Buffering=no. Клиент использует fetch POST и reader, не EventSource.
Событие — event, одна JSON-строка data и пустая строка; сетевой chunk не является
границей события. JSON экранирует переносы текста. Каждый event выполняет flush.

Первое событие — message.started после commit пары сообщений. Затем возможны
tool.started/tool.finished, answer.delta, action.proposed. Финальное Message
сохраняется до message.completed/message.failed. После terminal event — EOF.
Agent callbacks пишут в ограниченную очередь, не в ResponseWriter; обработчик
является единственным writer. Переиспользованные указатели событий копируются.

Heartbeat — 15 секунд, checkpoint текста — раз в секунду. Генерация — 120 секунд,
включая подготовку после резервирования; SQL/снимки перед SSE ограничены 5 секундами.
Контекст передаётся агенту/инструментам. Сетевая запись и отдельные SQL-проверки
ограничены 5 секундами. Общего WriteTimeout, обрезающего SSE, нет. Прокси должен
отключить buffering и иметь read timeout больше 120 секунд.

Assistant: generating → completed/failed/interrupted. Disconnect/остановка отменяют
Run и сохраняют interrupted/GENERATION_INTERRUPTED; timeout — failed/GENERATION_TIMEOUT.
Для финального сохранения выделяется отдельный контекст до 5 секунд с Principal
и trace_id. Ошибки до SSE возвращаются JSON; после начала JSON не дописывается.
Ошибка сообщения читается как message.error.error.code. Внутренние SQL/LLM ошибки
не раскрываются. Если финал сохранить нельзя, terminal success не отправляется;
в логе фиксируется trace_id. Поздняя отмена не меняет сохранённый terminal.

EOF без terminal не означает успех. Фронт восстанавливает GET messages; пока
generating — polling раз в 2 секунды до 130 секунд, затем ошибка и ручной GET.
Автоматического повторного запуска или SSE replay/Last-Event-ID нет. Осознанный
повтор failed/interrupted — новый UUID.

При старте после миграций до приёма запросов generating становятся interrupted,
частичный текст/источники/связи сохраняются. Completed не затрагивается. MVP — один
API-процесс на схему БД. Session-level advisory lock в Run запрещает второй запуск,
который мог бы прервать чужие живые генерации. Это не механизм HA.

## Подключение Ромы и №14

Composition root принимает `app.Dependencies{Access: provider, Agent: agent,
Requests: requestReader}`. Передайте его в NewHandler из Run вместо пустого
Dependencies. В DEMO_MODE явно выбирается demo.Agent; вне demo без Agent отправка
даёт 503 до принятия вопроса. Ошибка настоящего агента не включает fallback.
DEMO_CHAT_DELAY (default 250ms, 0s..5s) влияет только на синтетические chunks;
для kill smoke CI использует 1s.

AgentInput: доверенные conversation/assistant/step IDs, Text, History, Requests.
Текущий вопрос не дублируется в History. Берутся последние целые завершённые пары;
вместе с текущим вопросом — до 20 сообщений/24000 code points. Полная GET-история
сохраняет также неудачные ответы. Agent обязан соблюдать ctx и вызывать emit
последовательно, не используя его после возврата Run.

Разрешены answer.delta, tool.started/tool.finished, draft.saved. Вызовы инструмента
связаны tool_call_id; недопустимые события завершают ответ без передачи внутренних
payload/рассуждений. AgentResult возвращает собранные Sources/RequestIDs также при
ошибке. Чат не добавляет агенту Submit/confirm.

№14 поставляет SQL Store для drafts.Service. LinkAssistant вызывает
`chat.LinkRequestTx(ctx, tx, owner, assistantID, requestID)` в той же транзакции
после проверки/сохранения заявки. Порядок блокировок: план → заявка → диалог → assistant.
Helper проверяет владельца/роль и generating assistant, не делает commit. Принадлежность
request_id и допустимость версии/изменения самой заявки проверяет request-service.

На draft.saved чат перечитывает RequestReader.Get под Principal, проверяет
сохранённую связь и conversation/assistant/step. Событие не считается доказательством
commit. Только актуальный draft выдаёт action.proposed с полной карточкой OpenAPI.
Уже отправленную заявку повторно не предлагает. Финал добавляет проверенные
RequestIDs, не затирает committed связи при сбое модели. Ссылки на активную заявку
из другого своего диалога допустимы в финале после проверки владельца Reader.

SQL-тесты используют отдельную fixture_drafts, подтверждая commit/rollback draft+link
и сохранение после ошибки модели. Это проверка стыка, не готовый Store/confirm/worker
из №14. Demo-агент возвращает пустые sources/request_ids, черновиков не создаёт.

## Проверка

Из backend с TEST_DATABASE_URL:

```powershell
go mod verify
go vet ./...
go test -race -count=1 -timeout=2m ./...
go build ./...
```

Проверяются права, UUID/конкуренция, порядок,
настоящие HTTP flush/disconnect, управляемые таймеры, terminal-before-event,
panic/невалидные события, атомарные связи и recovery. В CI PostgreSQL обязателен.

Полный Docker smoke из корня, Python 3 использует только stdlib:

```powershell
$env:DEMO_CHAT_DELAY = '1s'
docker compose up --build --wait api
docker compose --profile demo run --rm seed
python scripts/smoke_chat.py prepare
docker compose restart api
python scripts/smoke_chat.py verify
python scripts/smoke_chat.py crash --docker
docker compose up -d --wait api
python scripts/smoke_chat.py recover
```

Проверяется SIGKILL после checkpoint, восстановление interrupted, сохранность
completed, отказ повтора UUID и новая генерация. Demo-cookie хранится в игнорируемом
tmp-файле и удаляется после успеха. Интеграция с React и настоящей моделью — следующий этап.

Локальная приемка 10 ноября 2026: mod verify/vet/build/race — PASS; настоящие
PostgreSQL-тесты выполнены без пропуска. Отдельный Windows Go-процесс прошёл
prepare → restart → verify → принудительное завершение → recover. Проверено также,
что второй API-процесс отклоняется, а первый остаётся ready. Покрытие всех Go-пакетов
— 82.6% statements, не метрика строк из ТЗ. Linux-бинарники собраны с CGO_ENABLED=0;
контейнерный сценарий выполняется в GitHub Actions.
