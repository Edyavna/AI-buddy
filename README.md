# AI Buddy

Прототип помощника для адаптации стажёров в Авито. Интерфейс — React, сервер — Go, данные — PostgreSQL с pgvector. Основная интеграционная ветка — `dev`.

## Возможности и границы прототипа

- Стажёр видит план адаптации, зависимости, подтверждения и следующий доступный шаг.
- Чат хранит диалоги, учитывает историю, поддерживает архив и восстановление.
- В режиме live помощник ищет доступные документы, показывает источники и помогает подготовить заявку на доступ. Отправка заявки требует подтверждения пользователя.
- Рабочие вопросы направляются наставнику, кадровые — HR. Есть очередь действий, ответы и пополнение базы знаний Markdown-документами.
- Наставник может общаться с ИИ и готовить Markdown-инструкции в отдельном режиме, уточнять текст и скачивать версии. Публикация выполняется отдельной загрузкой.
- Персональная память стажёра сохраняет подтверждённые предпочтения из серверного каталога. Записи можно исправлять и удалять.
- Встречи поддерживают приглашения из чата, выбор повестки, решение наставника, перенос, итоги, договорённости и экспорт принятой встречи в .ics.
- Есть подготовка к ревью и обзор адаптации для куратора.
- Публичные знания включают импортируемые AvitoTech Playbook и снимок статьи «Авито» из Википедии.

Корпоративные пользователи, назначения, задачи и выдача прав в поставке демонстрационные. Настоящее право на корпоративный ресурс демосервис не выдаёт. Корпоративный SSO и автоматическая синхронизация календаря требуют отдельных интеграций. GitHub-интеграция подготовки к ревью имеет отдельные настройки в документации модуля.

## Что установить

Для основного запуска нужны Git, Docker Engine с Docker Compose v2 или Docker Desktop в режиме Linux containers и браузер. На Windows включите WSL 2 и запустите Docker Desktop. На macOS используйте Docker Desktop; на Linux — Engine и Compose plugin.

Проверьте в терминале:

```sh
git --version
docker version
docker compose version
docker compose up --help
```

Команда `docker version` должна показывать и Client, и Server. Compose должен поддерживать `up --wait`. Для override-примера ниже нужна поддержка тега !override; инструкция проверена с Compose v5.6.0. Интернет нужен для клонирования, образов и зависимостей при первой сборке. Для live также нужен доступ к API модели и эмбеддингов. GPU не требуется.

Docker сам устанавливает зависимости:

| Компонент | Версия в репозитории |
| --- | --- |
| Go в Docker | 1.26.5, Alpine |
| Минимальная версия Go в go.mod | 1.26.0 |
| Node.js в Docker | 24.15.0, Alpine |
| PostgreSQL / pgvector | образ pgvector/pgvector:0.8.7-pg17 |
| React | 19.3.1 |
| TypeScript | 5.9.3 |
| pgx | v5.11.0 |
| Backend runtime | Alpine 3.23 |
| Nginx | stable-alpine, плавающий тег |

Точные npm-зависимости закреплены в `frontend/package-lock.json`, Go-зависимости — в `backend/go.mod` и `backend/go.sum`. Сборка использует `npm ci`. Теги PostgreSQL и Nginx могут получать обновления образов, поэтому бинарная воспроизводимость между датами не гарантируется. Go и Node на хосте нужны только для разработки и локальных тестов.

## Первый запуск без API-ключей

Все команды выполняйте из корня клона, где лежит `compose.yaml`.

```sh
git clone --branch dev https://github.com/Edyavna/AI-buddy.git
cd AI-buddy
git rev-parse HEAD
```

Сохраните SHA последней команды, если нужно воспроизвести конкретную версию. Для приватного репозитория понадобится доступ к GitHub.

Создайте локальный файл настроек один раз.

PowerShell:

```powershell
Copy-Item .env.example .env
```

Bash / zsh:

```sh
cp .env.example .env
```

Оставьте `AI_MODE=demo`, `DEMO_MODE=true`. Запустите:

```sh
docker compose config --quiet
docker compose up --build -d --wait --wait-timeout 180 frontend
docker compose --profile demo run --rm seed
docker compose ps -a
```

Compose поднимает БД, применяет миграции, запускает API и Nginx с собранным React. `seed` создаёт тестовые аккаунты и планы. БД, API и frontend должны стать healthy. У одноразового migrate нормальное состояние — Exited (0). Если команда завершилась ошибкой, сначала прочитайте раздел диагностики.

Откройте **http://localhost:5173**. На экране входа выберите аккаунт, пароль демоаккаунта не требуется:

| ID | Назначение |
| --- | --- |
| intern_demo_1 | Стажёр backend-команды |
| mentor_demo_1 | Его наставник, также куратор backend-команды |
| hr_demo_1 | HR, обслуживающий backend- и frontend-команды |
| intern_demo_2 / mentor_demo_2 | Вторая пара для проверки изоляции |

Дополнительные аккаунты предназначены для проверки отсутствующих назначений и чужих команд. Демо-вход не подходит для публичного размещения с реальными данными.

В demo используются синтетические ответы и тестовая логика. Для свободного диалога, генерации инструкций и поиска с внешней моделью выполните настройку live ниже. Успешный demo-запуск не проверяет доступ к DeepSeek/Jina.

Проверка HTTP в PowerShell:

```powershell
Invoke-RestMethod http://localhost:8080/healthz
Invoke-RestMethod http://localhost:8080/readyz
(Invoke-WebRequest http://localhost:5173).StatusCode
```

В Bash / zsh:

```sh
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
curl --fail -I http://localhost:5173
```

## Настройка реальной модели

В `.env` замените режим и заполните все пять параметров. Значения в угловых скобках нужно заменить своими:

```dotenv
DEMO_MODE=true
AI_MODE=live
LLM_BASE_URL=https://api.deepseek.com
LLM_MODEL=deepseek-flash
LLM_API_KEY=<ключ модели>
EMBEDDING_BASE_URL=https://api.jina.ai/v1
EMBEDDING_API_KEY=<ключ Jina>
```

При проверке проекта использовался идентификатор модели DeepSeek `deepseek-flash`. Для векторного поиска сервер использует модель Jina `jina-embeddings-v3` с размерностью 1024; её имя и размерность заданы в коде. Доступность и идентификатор модели DeepSeek проверяйте в своём API-аккаунте. Комментированный пример из .env.example не подтверждает доступность модели для вашего ключа. Адаптер ожидает совместимые chat completions, JSON-ответы и инструменты. Самовольная замена провайдера может потребовать изменения адаптера.

`DEMO_MODE=true` обязателен и при `AI_MODE=live`: текущая сборка подключает реальную модель к демонстрационным корпоративным сервисам. Внутри Compose `DATABASE_ENABLED=true` задан явно.

Примените настройки и импортируйте знания:

```sh
docker compose up --build -d --wait --wait-timeout 180 frontend
docker compose --profile demo run --rm seed
docker compose --profile demo run --rm knowledge-import
docker compose --profile demo run --rm knowledge-import /app/knowledge-import -wikipedia-dir /app/wikipedia
```

Первый импорт загружает синтетические инструкции из `demo/knowledge`, второй — зафиксированную статью из `demo/wikipedia`. Импорт вызывает API эмбеддингов и может расходовать баланс. Повторный импорт неизменённой редакции Википедии не создаёт дубликаты. Успешный /readyz проверяет БД, но не работоспособность внешних API.

Войдите стажёром, создайте новый диалог и спросите «Кто ты?», затем «Когда основали Авито?». Второй вопрос проверяет публичный поиск и должен показывать источник статьи. Для проверки обычного агента спросите об инструкции запуска учебного проекта. Реальные ответы модели могут различаться.

Тексты запросов и извлечённые фрагменты передаются настроенным внешним провайдерам. Используйте демоданные. Ключи храните только в локальном .env и не публикуйте его.

## Импорт AvitoTech Playbook

Playbook импортируется отдельно, после live-настройки и миграций. В соседнюю папку клонируйте источник:

```sh
git clone https://github.com/avito-tech/playbook.git ../AvitoTech-playbook
git -C ../AvitoTech-playbook rev-parse HEAD
```

Скопируйте полный SHA и подставьте вместо `FULL_COMMIT_SHA`:

```sh
docker compose --profile playbook run --rm --build playbook-import -repo /playbook -commit FULL_COMMIT_SHA -check
docker compose --profile playbook run --rm playbook-import -repo /playbook -commit FULL_COMMIT_SHA
```

Первая команда проверяет снимок без записи в БД и вызова Jina. Вторая индексирует его. Путь по умолчанию — `../AvitoTech-playbook`; для другого пути задайте `PLAYBOOK_REPO` в .env. Docker должен иметь доступ к этой папке. Публичный Playbook содержит исторические сведения; актуальные правила команды поступают из её документов.

## Окружение и порты

| Настройка | Значение / назначение |
| --- | --- |
| POSTGRES_USER / POSTGRES_PASSWORD / POSTGRES_DB | buddy / buddy_dev_only / buddy для локального демо |
| ALLOWED_ORIGINS | http://localhost:5173,http://localhost:8080 |
| SESSION_COOKIE_SECURE | false для локального HTTP |
| SESSION_TTL | 24h |
| STARTUP_TIMEOUT / SHUTDOWN_TIMEOUT | 15s / 10s |
| DEMO_CHAT_DELAY | 250ms |
| AI_MODE | demo или live |
| HTTP_ADDR | В Compose принудительно :8080 |
| DATABASE_URL | Compose строит адрес с хостом db из POSTGRES_* |

Порты хоста: 5173 — сайт, 8080 — API, 5432 — БД. Они привязаны к 127.0.0.1. Открывайте сайт через localhost. API и SSE доступны через Nginx на том же адресе сайта.

Go, запущенный через `go run`, читает переменные процесса и сам .env не загружает. Значение DATABASE_URL из .env.example относится к локальному Go. Контейнеры получают свой адрес БД из Compose. Изменение POSTGRES_* после создания тома не меняет автоматически пользователей и пароль существующей БД.

Если порт занят, остановите конкурирующий сервис либо создайте `compose.override.yaml`:

```yaml
services:
  db:
    ports: !override ["127.0.0.1:55433:5432"]
  api:
    ports: !override ["127.0.0.1:8081:8080"]
  frontend:
    ports: !override ["127.0.0.1:5174:80"]
```

Используйте Compose с поддержкой !override (проверено на v5.6.0). В .env установите `ALLOWED_ORIGINS=http://localhost:5174,http://localhost:8081`. Повторите команду запуска; сайт будет на http://localhost:5174. Внутренние порты контейнеров остаются заданными в compose.yaml.

## Остановка, повторный запуск и обновление

```sh
docker compose stop
docker compose up -d --wait frontend
```

Для обновления чистого клона:

```sh
git switch dev
git pull --ff-only origin dev
docker compose up --build -d --wait --wait-timeout 180 frontend
docker compose --profile demo run --rm seed
```

Если Git сообщает о локальных изменениях, сохраните их перед обновлением. Повтор seed вставляет отсутствующие демонстрационные данные и сохраняет существующие записи. После обновления публичных источников выполните соответствующие команды импорта.

`docker compose down` удаляет контейнеры и сеть, сохраняя именованный том БД. `docker compose down -v` удаляет также данные — используйте только для намеренного полного сброса тестовой установки. Для резервной копии можно выполнить `pg_dump` внутри db и скопировать файл через `docker cp`.

## Как проверить заявки

Стажёр просит доступ к учебному репозиторию, проверяет черновик и нажимает подтверждение. Для демонстрации решения внешнего сервиса:

```sh
docker compose exec api /app/demo-control list
docker compose exec api /app/demo-control approve REQUEST_ID
```

Вместо REQUEST_ID подставьте идентификатор из списка. Worker синхронизирует статус; обновите карточку заявки. Pending означает ожидание согласования. Команда approve изменяет только демонстрационный сервис.

## Тесты для разработчика

Для локальных команд установите Go 1.26.5 и Node.js 24.15.0. Для проверки контрактов используйте Python 3.13 (версия CI). Зависимости закреплены в contracts/requirements-check.txt. На Windows вместо python используйте `py -3`.

Из корня:

```sh
docker compose --profile test up -d --wait db-test
```

Задайте тестовую БД в терминале с Go. Она использует отдельный порт 5433.

PowerShell:

```powershell
$env:TEST_DATABASE_URL='postgres://buddy:buddy_test_only@localhost:5433/buddy_test?sslmode=disable'
```

Bash / zsh:

```sh
export TEST_DATABASE_URL='postgres://buddy:buddy_test_only@localhost:5433/buddy_test?sslmode=disable'
```

Затем:

```sh
cd backend
go mod verify
go vet ./...
go test -count=1 ./...
go build ./...
cd ../frontend
npm ci
npm run check:api
npm run typecheck
npm run lint
npm run test:coverage
npm run test:e2e
npm run build
cd ..
python -m pip install -r contracts/requirements-check.txt
python scripts/check_contracts.py
```

Без TEST_DATABASE_URL интеграционные PostgreSQL-тесты пропускаются. Тесты создают собственные временные базы, поэтому используйте отдельный тестовый сервер с правом CREATEDB. Для `go test -race ./...` нужен поддерживаемый C-компилятор; Linux CI выполняет race-проверки. Тесты провайдеров используют управляемые ответы, настоящие ключи для них не нужны. Команда npm run test:e2e запускает Vitest-сценарии; это не проверка реального браузера с живыми провайдерами. Python-зависимости рекомендуется устанавливать в отдельное виртуальное окружение (python -m venv .venv).

Отчёт Vitest находится в `frontend/coverage`. Полные команды Go-покрытия и актуальные пороги находятся в `.github/workflows/backend.yml`, фронтенда — в `.github/workflows/frontend.yml`. Числа покрытия из старых отчётов относятся к указанным в них коммитам.

## Диагностика

```sh
docker compose ps -a
docker compose logs --tail 100 migrate api frontend
docker compose logs --tail 100 db
```

- Docker daemon недоступен — запустите Docker Desktop/Engine и повторите docker version.
- Port is already allocated / ports are not available — освободите порт или используйте override выше.
- Нет аккаунтов на входе — проверьте успешное выполнение seed.
- Migrate завершился с ошибкой — прочитайте его лог и лог db. Не редактируйте применённые SQL-миграции и их контрольные суммы без установления причины.
- API не стартует в live — проверьте все пять параметров провайдеров и DEMO_MODE=true.
- Ответ об отсутствии знаний — проверьте успешность импорта и доступность API эмбеддингов.
- 401/403 после изменения адреса — проверьте ALLOWED_ORIGINS, локальный HTTP с SESSION_COOKIE_SECURE=false и войдите снова.
- Образы или npm/Go-модули не скачиваются — проверьте сеть, прокси Docker и доступ к реестрам. API-ключи не исправляют ошибки загрузки образов.
- Изменения .env не применились после restart — выполните `docker compose up -d --wait frontend`, чтобы Compose пересоздал сервисы с новой конфигурацией.

## Дополнительные документы

- [Контракт API](contracts/openapi.yaml)
- [Публичная Википедия](docs/backend/wikipedia.md)
- [AvitoTech Playbook](docs/backend/playbook.md)
- [Персональная память](docs/features/personal-memory.md)
- [Чат наставника](docs/features/mentor-chat.md)
- [Markdown-инструкции](docs/features/mentor-markdown.md)
- [Приглашения на встречи](docs/features/meeting-invitations.md)
- [Подготовка к ревью](docs/features/review-preparation.md)
- [Подготовка к защите](docs/defense-checklist.md)

README описывает поведение исходного кода. Успешный запуск на конкретном устройстве зависит от доступности образов, свободных портов, настроенного Docker и, для live, действующих ключей провайдеров.

## Комплект для защиты

[Оглавление комплекта для защиты](docs/README.md).
