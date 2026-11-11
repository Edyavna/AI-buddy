# Вопрос наставнику из чата

Feature стажёра для согласованного API `/api/v1/messages/{message_id}/knowledge-gap`.
GET возвращает `{question: InternKnowledgeGap | null}`; POST без тела создаёт запись
(201) или возвращает прежнюю (200). Текущие OpenAPI и generated types уже содержат
контракт, поэтому отдельных mock-only DTO здесь нет.

## Подключение во втором PR

`ChatPage` и `ChatController` намеренно не изменены. Владелец чата передаёт колбэки
и вычисляет `canEscalate` только по структурированному результату завершённого ответа:

```tsx
const canEscalate = message.role === 'assistant'
  && message.status === 'completed'
  && message.outcome === 'knowledge_gap';

<MentorQuestionPanel
  messageId={message.id}
  canEscalate={canEscalate}
  isChatBusy={busy}
  onOpenDocument={setDocumentId}
  onRepeatQuestion={() => {
    // Существующий ChatController.send создаёт новый client_message_id.
    void controller.send(originalQuestion, userId, stepId, onConversationCreated);
  }}
/>
```

`onOpenDocument(documentId)` должен открыть общий `DocumentDrawer`.
`onRepeatQuestion()` вызывает существующую отправку ровно один раз; компонент не хранит
текст исходного вопроса и не создаёт идентификаторы сообщений. `isChatBusy` блокирует
повтор во время другой генерации. Не передавать `canEscalate=true` для `answered`,
`needs_clarification`, `unknown`, незавершённых или технически failed/interrupted ответов.

## Состояние и сессия

Все копии компонента используют `queryKeys.mentorQuestion(messageId)` и один общий
in-flight POST. При network-ошибке POST сначала выполняется GET; только найденная
запись считается успешной передачей. Повторный POST использует прежний `messageId`.

`new` и `in_progress` опрашиваются раз в 5 секунд, пока карточка на экране и вкладка
видима. При возвращении на вкладку сверяется любая существующая запись, включая
`resolved`, чтобы показать повторное открытие наставником. Меньшая `revision` не
перезаписывает кеш. 401/403/404 скрывают данные и действия; network/503 сохраняют
последнее известное состояние с ошибкой обновления.

`resetSessionState` отменяет in-flight POST/сверку через `cancelMentorQuestionRequests`,
затем общая сессионная очистка отменяет GET и удаляет кеш. Данные между аккаунтами
не сохраняются.

## Моки

`createMentorQuestionHandlers()` включён в общий `createHandlers()`. Для сообщения
`message_gap` GET до передачи возвращает null, POST создаёт `new`, следующие GET
детерминированно переводят запись в `in_progress` и `resolved` с `doc_local_setup`.
Повторный POST возвращает прежнюю запись. Это имитация API, а не проверка Go/LLM.
