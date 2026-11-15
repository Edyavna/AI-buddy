# Чат наставника — #76 / PR #77

Вкладка «Чат» открывает `/mentor/chat`, существующий диалог —
`/mentor/chat/:conversationId`. Используются общий интерфейс чата, список, история,
архив, восстановление SSE, Markdown и поле ввода. Название диалога сохраняется после
первого ответа; при отсутствии отдельного генератора берётся сокращённая первая реплика.

## Модель и права

Наставник создаёт обычный диалог с `step_id: null`. Передача шага в create API даёт 422;
параметры шага/адресата в URL кабинета наставника игнорируются. Список, прямое чтение,
история, отправка и архив проверяют владельца и команду. Другой наставник или стажёр
получает 404 на чужой объект, HR — 403 на API чата.

В live-режиме `agent.Mentor` вызывает существующую настроенную модель через
`CompleteWithoutTools`. Передаются системная инструкция, завершённые пары собственных
реплик и текущий вопрос. Общие лимиты истории сохраняются: до 19 прошлых сообщений и
24000 символов вместе с текущим вопросом. Незавершённые/ошибочные пары в контекст
не включаются. Модель возвращает JSON с `answer`, отображаемым как Markdown.
В явно включённом demo-режиме работает `demo.MentorAgent`.

Чат наставника не читает обращения/заявки стажёра и не зависит от их доступности.
Обработка заголовка выбирается у того же исполнителя, что ответ. Инструменты стажёра
не передаются модели наставника; формы заявок и обращения к специалистам остаются
в кабинете стажёра. Экспорт Markdown относится к #75.

Сбой модели сохраняется в истории как failed и допускает явный «Повторить запрос».
Повтор использует исходный текст и новый client_message_id; неизвестный результат
отправки/создания сверяется с историей по действующим правилам общего чата.

## Исправления ревью

База — `dev 2612426`, включающая #81 и #72. Конфликты `service.go`, `ChatPage.tsx` и
`AppShell.tsx` разрешены с сохранением очередей действий, архива, адресата помощи стажёру,
контекста обращений и рекомендаций. Новых миграций у этой поставки нет.

Устранены зависимость наставника от запросов обращений стажёра, выбор чужого обработчика
заголовка и panic ненастроенной модели. При 401/403/404 чтения, отправки или архивирования
чат очищает свои кеши, локальную историю и черновики, отменяет предыдущее поколение
запросов. Поздний 200 и следующая 503 не возвращают закрытый текст. Новый успешный
авторизованный запрос может восстановить историю. Выход/смена аккаунта дополнительно
используют существующую очистку сессии. На широком экране навигация наставника перенесена
на отдельную строку: имя и команда остаются внутри шапки.

## Контракт и пример

Используются те же маршруты `/api/v1/conversations` и `/messages`, cookie-сессии,
проверка Origin, ApiError и SSE. OpenAPI описывает роли и запрет step_id для наставника.

```http
POST /api/v1/conversations
Content-Type: application/json

{"client_conversation_id":"12345678-1234-4234-8234-123456789076"}
```

Затем отправить в `/api/v1/conversations/{id}/messages`:

```json
{"client_message_id":"12345678-1234-4234-8234-123456789077","text":"Помоги подготовить встречу со стажёром"}
```

## Проверка из чистой среды

Команды для bash из корня, с отдельным Compose project и свободными стандартными портами.
Для PowerShell переменная задаётся как `$env:COMPOSE_FILE`.

```bash
export COMPOSE_FILE=compose.yaml:scripts/compose.hr-test.yaml
docker compose -p mentor-chat-76 up --build --wait --wait-timeout 180 frontend hr-fixture
docker compose -p mentor-chat-76 --profile demo run --rm seed
npm install --no-save --package-lock=false playwright-core@1.60.0
npx playwright-core install --with-deps chromium
node scripts/mentor-chat-browser.mjs
docker compose -p mentor-chat-76 restart api hr-fixture
docker compose -p mentor-chat-76 up -d --wait api hr-fixture
docker compose -p mentor-chat-76 --profile demo run --rm seed
node scripts/mentor-chat-browser.mjs --verify
```

Сценарий проверяет 360/768/1440 px, SSE и сохранённую историю, продолжение с контекстом,
reload/прямой URL, архив и возврат с клавиатуры, повтор после сбоя модели, изоляцию второго
наставника и смену аккаунта на стажёра с сохранением его сценария чата. `--verify` проверяет
сохранность после restart/reseed. Скриншоты и синтетические ID — `tmp/mentor-chat-browser/`;
CI публикует артефакт `mentor-chat-browser`.

Можно задать `MENTOR_APP_URL`, `MENTOR_BROWSER`, `MENTOR_PLAYWRIGHT_MODULE`.
Локально проверены Chrome, Go, PostgreSQL и Vite; CI повторяет тот же сценарий через
production Nginx SPA. Внешняя модель заменена HTTP-фикстурой: она получает фактическую
историю от Go и отклоняет инструменты в запросе наставника. Живая модель с ключом
не проверялась. Сырые SSE-события отдельно проверяет PostgreSQL/API-тест.

## Проверки и покрытие

Frontend: `typecheck`, `lint`, `test:coverage`, `test:e2e`, `build`, `check:api`.
Локально при работающем Docker для покрытия использован `--maxWorkers=2`.
Backend: `go test -race -count=1 -timeout=2m -coverpkg=./... -coverprofile=coverage.out ./...`,
`go vet ./...`, `go build ./...`; тестовая PostgreSQL с pgvector и правом CREATEDB.
Для Docker Desktop локально применялись `-p 1 -timeout=5m`; настройки CI не увеличены.
Контракты: `python scripts/check_contracts.py` после установки `contracts/requirements-check.txt`.

Из frontend/ и backend/ соответственно:

```bash
node ../scripts/hr-coverage.mjs origin/dev frontend
mkdir -p coverage
go run ../scripts/go_line_coverage.go --lines coverage.out > coverage/lines.tsv
node ../scripts/hr-coverage.mjs origin/dev backend
```

Обе проверки строго >90% включены в CI; общие пороги сохранены. Generated, тесты,
моки и зависимости исключаются действующими правилами, рабочая логика измеряется.
[Frontend-строки](issue76-coverage/frontend.txt), [backend-строки](issue76-coverage/backend.txt),
[команды и результаты](issue76-coverage/results.txt). Существующее предупреждение сборки
о JS-файле больше 500 КБ осталось.
