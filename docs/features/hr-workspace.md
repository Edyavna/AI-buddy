# HR-кабинет и кадровые обращения

Стажёр открывает «Обратиться к специалисту» под ответом в чате. Перед отправкой он видит предложенное направление и может изменить адресата, вопрос и контекст. Если у команды пока нет HR, черновик сохраняется на сервере. Смешанный вопрос можно разделить на два обращения. При смене направления новый специалист получает только подтверждённый контекст текущего этапа. Предыдущие ответы сохраняются в истории автора.

HR видит кадровые обращения обслуживаемых команд, берёт обращение в работу, отвечает и решает его. Ответ можно дополнить опубликованной инструкцией. Редактировать, публиковать и отзывать инструкцию могут HR, обслуживающие все команды текущей аудитории. Обновление инструкции становится доступно после индексации новой версии.

## Запуск

```sh
docker compose up --build --wait frontend
docker compose --profile demo run --rm seed
docker compose run --rm api /app/hr-scope list
docker compose run --rm api /app/hr-scope revoke hr_demo_1 team_demo_frontend
docker compose run --rm api /app/hr-scope grant hr_demo_1 team_demo_frontend
```

Интерфейс доступен на `http://localhost:5173`. Анна обслуживает backend и frontend. Денис обслуживает backend. У третьей demo-команды нет HR. Повторный seed сохраняет ранее отозванные привязки, обращения, документы и версии.

Для классификации, ответов по инструкциям и индексации в живом режиме настройте переменные `AI_MODE=live`, `LLM_BASE_URL`, `LLM_MODEL`, `LLM_API_KEY`, `EMBEDDING_BASE_URL`, `EMBEDDING_API_KEY` по `.env.example`. Для воспроизводимого прогона доступны детерминированные HTTP-провайдеры.

## Пример подтверждения

```http
POST /api/v1/messages/ASSISTANT_MESSAGE_ID/questions
Content-Type: application/json
Origin: http://localhost:5173

{
  "operation_id": "87654321-1234-4321-9876-000000000001",
  "direction": "hr",
  "question": "Как оформить отпуск?",
  "context_excerpt": "Хочу уточнить порядок подачи заявления."
}
```

Запрос использует cookie стажёра. При неясном исходе клиент сверяет сохранённое обращение с отправленным снимком. Повтор того же ключа безопасен после успешной записи. `GET /questions/{id}` возвращает автору текущий этап и историю. Объекты вне области возвращают 404.

## Приёмка

```sh
docker compose -f compose.yaml -f scripts/compose.hr-test.yaml up --build --wait frontend hr-fixture
docker compose -f compose.yaml -f scripts/compose.hr-test.yaml --profile demo run --rm seed
npm install --no-save --package-lock=false playwright-core
npx playwright-core install chromium
HR_RESTART_COMPOSE=1 node scripts/hr-browser.mjs
```

Сценарий открывает настоящие браузерные контексты для стажёра, HR и наставника. Скриншоты сохраняются в `tmp/hr-browser`. При локальном запуске Go и Vite можно передать `HR_APP_URL`, путь к браузеру через `HR_BROWSER` и команду перезапуска через `HR_RESTART_SCRIPT`.

Проверка живой модели использует размеченные синтетические вопросы из `demo/hr-routing-eval.json`.

```sh
cd backend
go run ./cmd/hr-routing-eval ../demo/hr-routing-eval.json
```

Указать точный идентификатор версии модели через `LLM_MODEL`. Успех требует минимум 36 верных ответов из 40 в каждой из четырёх категорий и отсутствия кадровых вопросов, направленных наставнику. Отчёт печатает матрицу ошибок, долю уточнений и параметры прогона.

Обычные тесты используют настоящий PostgreSQL при заданном `TEST_DATABASE_URL`. Для отчёта по изменённым строкам сохраните Go coverage profile как `backend/coverage.out` и Vitest LCOV как `frontend/coverage/lcov.info`.

```sh
(cd backend && mkdir -p coverage && go run ../scripts/go_line_coverage.go --lines coverage.out > coverage/lines.tsv)
node scripts/hr-coverage.mjs b514933 backend
node scripts/hr-coverage.mjs b514933 frontend
```

Каждый слой проходит порог при покрытии строго больше 90% изменённых исполняемых строк.
