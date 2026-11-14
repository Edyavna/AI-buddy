# AI-buddy

Прототип ИИ-помощника для адаптации стажеров. Стек: Go, React + TypeScript,
PostgreSQL и внешний LLM API. Реализованы Go-каркас, подключение БД, миграции,
внутренние интерфейсы, серверные сессии, сохраняемый план адаптации и HTTP/SSE-чат.
Также реализованы React UI-каркас и API-клиент с тестовыми имитациями.
Корпоративные сервисы работают на сохраняемой демо-имитации. Агент и поиск можно подключить через DeepSeek и Jina.

Основной сценарий: первая задача → инструкции команды → отсутствие доступа →
подтверждение заявки → сохраненный номер и статус → следующий шаг.

## Запуск через Docker Compose

Нужен Docker с Compose v2, поддерживающим `up --wait`. Из корня репозитория
запустите React, Go и PostgreSQL и заполните базу синтетическими данными.

```sh
docker compose up --build --wait frontend
docker compose --profile demo run --rm seed
```

Откройте [http://localhost:5173](http://localhost:5173) и выберите тестового
стажёра или наставника на экране входа. Docker собирает React и Go, применяет
миграции и дожидается готовности API. По умолчанию используется `AI_MODE=demo`
с синтетическим ответом. Ключи и отдельная установка Go или Node не требуются.

Nginx выдаёт React и передаёт `/api` в Go с того же адреса. Буферизация ответов
отключена, а таймаут ожидания данных от API составляет 150 секунд. Внешний
адрес интерфейса должен входить в `ALLOWED_ORIGINS` на стороне Go.

Для обновления повторите обе команды запуска. `docker compose down` останавливает
стек и сохраняет том PostgreSQL. Для разработки с Vite запустите только сервис
`api`, как описано ниже. Vite и контейнер React используют один порт.

## Запуск для разработки

Требуется **Go 1.26.5**. Команды ниже выполняются из корня репозитория `AI-buddy`,
где находится этот README, а не из родительской папки `AvitoHackathon`.

### Windows без Docker и PostgreSQL

```powershell
$env:DEMO_MODE = 'true'
$env:DATABASE_ENABLED = 'false'
Set-Location backend
go run ./cmd/server
```

В другом терминале:

```powershell
Invoke-RestMethod http://localhost:8080/healthz
```

`/healthz` возвращает `200 {"status":"ok"}`. `/readyz` возвращает **503**, поскольку
БД отключена. В этом режиме бизнес-маршруты возвращают 404 ApiError. Этот режим проверяет
только HTTP-каркас; данные приложения в нем не сохраняются. Остановка — Ctrl+C.

### Запуск Go и PostgreSQL через Docker Compose

Нужен Docker с Compose v2, поддерживающим `up --wait`.

```powershell
Copy-Item .env.example .env
docker compose config --quiet
docker compose up --build --wait api
docker compose --profile demo run --rm seed
```

Compose дожидается PostgreSQL, отдельно применяет миграции и запускает Go.
`http://localhost:8080/healthz` и `/readyz` должны отвечать 200.
Тома БД сохраняются при `docker compose down` и перезапуске.

```powershell
docker compose run --rm migrate
docker compose restart api
docker compose logs api
docker compose down
```

`.env.example` содержит только локальные демонстрационные значения. Compose читает
`.env` автоматически и использует имя БД `db` внутри сети. Go при локальном запуске
читает переменные процесса, а не файл `.env`. Подробный запуск с отдельной БД,
настройки и тесты: [backend/README.md](backend/README.md).

### Проверки без Docker

```powershell
Set-Location backend
go mod verify
go vet ./...
go test ./...
go build ./...
```

Реальные PostgreSQL-тесты требуют `TEST_DATABASE_URL`; без него они явно пропускаются.
`go test -race ./...` дополнительно требует C-компилятор на Windows; в Linux CI
race detector включен. Настоящий LLM-ключ для этих проверок не нужен.

## Документы

- [Контракт React ↔ Go](contracts/README.md)
- [OpenAPI](contracts/openapi.yaml)
- [Поток чата](contracts/chat-events.md)
- [Поведение API и правила повторов](contracts/behavior.md)
- [Общие примеры JSON](contracts/examples/scenario.json)
- [Начало работы фронтендеров](docs/frontend/README.md)
- [Задание FE1](docs/frontend/frontend-1.md)
- [Задание FE2](docs/frontend/frontend-2.md)
- [Стыки фронтенда](docs/frontend/integration.md)
- [Приемка и тестирование](docs/frontend/acceptance.md)
- [Внутренние Go-интерфейсы и передача участникам](docs/backend/interfaces.md)
- [Архитектура и порядок соединения модулей](docs/backend/architecture.md)
- [Задание Роме: агент и знания](docs/backend/roma.md)
- [Задание Егору: корпоративная имитация](docs/backend/egor.md)
- [SDD основного сценария](docs/sdd/access-onboarding.md)
- [Словарь проекта](CONTEXT.md)
- [Статус и приемка первого дня](docs/backend/day-1.md)

Исходное ТЗ находится в PDF в корне. Задания реализации ведутся в GitHub Issues.
Сессии, тестовые пользователи и сохраняемый план реализованы в №9.
Команды и примеры API: [сессии и план](docs/backend/session-onboarding.md).
Чат на тестовом агенте реализован в №10: [запуск и примеры](docs/backend/chat.md).
Заявки отправляются после подтверждения стажёром. Для подключения DeepSeek и Jina
в локальном .env выберите AI_MODE=live, заполните поля из .env.example и выполните:

```sh
docker compose up --build --wait frontend
docker compose --profile demo run --rm seed
docker compose --profile demo run --rm knowledge-import
```

В AI_MODE=demo остаётся синтетический SSE-ответ без внешних вызовов. См.
[конфигурацию](backend/README.md#конфигурация) и [импорт знаний](backend/internal/knowledge/README.md).

AvitoTech Playbook подключается отдельной командой. См. [импорт, обновление и вопросы для приёмки](docs/backend/playbook.md).

## Фронтенд и интеграция

Из папки frontend: npm ci, затем npm run dev. Интерфейс: http://localhost:5173.
Подробности и ограничения: [frontend/README.md](frontend/README.md).
Интеграционная ветка — dev; task-ветки объединяются в неё.
Готовая версия затем переносится в main.
