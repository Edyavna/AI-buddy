# Agent (#12)

Agent реализует ports.Agent. Зависимости задаёт сборка app: Knowledge (#11),
TaskProvider/AccessChecker (#13), DraftService/RequestReader (#14), Model.
Отправки/одобрения нет ни в интерфейсах зависимостей, ни в шести tool schemas.
История, вопрос, StepID и связанные заявки передаются модели; Principal только в ctx.
Документы считаются данными. Источники добавляются после успешного Read.

```go
a := &agent.Agent{Model: agent.FromEnv(), Knowledge: k, Tasks: tasks,
    Access: checker, Drafts: drafts, Requests: requests}
result, err := a.Run(ctx, input, func(ctx context.Context, event ports.AgentEvent) error {
    return chatCallback(ctx, event)
})
// result сохраняется и при err: там могут быть уже сохранённые draft IDs.
```

Run выдаёт tool.started/tool.finished, draft.saved после SaveAndLink и answer.delta
для конечного ответа. HTTP/SSE и persistence остаются в chat service Лёши.
DeepSeek запрашивается без stream: ответ поступает одним answer.delta после
tool loop; потокового чтения токенов провайдера в этой версии нет.

Конфигурация: LLM_BASE_URL=https://api.deepseek.com, LLM_MODEL из согласованной
конфигурации команды, LLM_API_KEY. Ключ только на сервере. FromEnv читает env,
не .env файл. HTTP timeout 30 секунд, Run до 120 секунд, 8 раундов/16 вызовов,
Rounds/Calls/Timeout настраиваются. Повтор одинаковых аргументов останавливает цикл.
64000 байт исходной истории, ограниченные ответы tools и provider HTTP.
Неизвестные поля и инструменты запрещены. Активная заявка другого диалога
возвращается без перепривязки; submitting/pending без draft.saved.

```sh
go test ./internal/agent ./internal/providerhttp -cover
go run ./cmd/agent-demo
```

agent-demo не обращается к платным API. Модель и сервисы в нём тестовые;
черновик живёт только во время запуска. Команда показывает последовательность
событий и итоговые Sources/RequestIDs. Реальный поиск по Markdown и PostgreSQL
в эту демонстрацию не входят.

TestOnboarding: S01/S02; TestInvalidCalls: S08/S19;
TestPartialAndEmit: S17/S20; TestStopBeforeTools: S18/S20;
TestRepeatAndNoKnowledge: S06/S19. Script — явный fake для CI, не live fallback.
Проверка фразы «подтверждаю» обеспечена отсутствием Submit в типе зависимостей.
Живой прогон с моделью, два разных реальных адаптера, HTTP/SSE и SQL DraftService
ещё требуют общей интеграции. Ключи и реальное качество ответов не проверены.
