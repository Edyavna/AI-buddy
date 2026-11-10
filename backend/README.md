# Go-бэкенд

Один Go module `github.com/Edyavna/AI-buddy/backend`, Go 1.26.5,
PostgreSQL 17, драйвер pgx v5.11.0; точные Go-зависимости в go.mod/go.sum.
Нет отдельного Python-сервиса. Python используется только валидатором контрактов.

## Структура

```text
cmd/server                 конфигурация, сигналы, запуск
cmd/migrate                отдельная команда применения миграций
cmd/seed                   идемпотентные синтетические аккаунты и планы
internal/auth              серверные сессии, me/logout, middleware
internal/onboarding        план, переходы и транзакционная связь с заявками
internal/chat              диалоги, HTTP/SSE, сообщения и recovery
internal/demo              seed и явная read-only заглушка AccessChecker
internal/app               сборка приложения и graceful shutdown
internal/platform/config   конфигурация из environment
internal/platform/database PostgreSQL pool через database/sql и unit of work
internal/platform/httpapi  JSON, ApiError, Origin, trace_id, recovery, health
internal/platform/identity доверенный Principal в context
internal/platform/migrations/sql  встроенные SQL-миграции
internal/ports             интерфейсы между командными модулями
internal/drafts            координатор атомарного сохранения draft + связи
internal/testkit           детерминированные заглушки без внешних API
```

HTTP не импортирует реализацию агента и адаптеров. Реальные модули подключаются
в composition root `internal/app`, будущие feature handlers регистрируются через
`httpapi.Options.Register`. Подробности: [интерфейсы](../docs/backend/interfaces.md).

## Конфигурация

| Переменная | Значение по умолчанию / правило |
|---|---|
| HTTP_ADDR | `:8080` |
| DATABASE_ENABLED | `true`; `false` разрешен только с DEMO_MODE=true |
| DATABASE_URL | Обязательный PostgreSQL URL при включенной БД |
| DEMO_MODE | `false`; включает accounts/session, seed запускается отдельно |
| ALLOWED_ORIGINS | `http://localhost:5173,http://localhost:8080` |
| STARTUP_TIMEOUT | `15s`, положительная Go duration |
| SHUTDOWN_TIMEOUT | `10s`, положительная Go duration |
| SESSION_TTL | `24h`, допустимо `1s..8760h` |
| SESSION_COOKIE_SECURE | `false`; `true` при HTTPS на доверенном proxy; прямой TLS включает автоматически |
| DEMO_CHAT_DELAY | `250ms`, `0s..5s`, только chunks синтетического агента |
| TEST_DATABASE_URL | Только тесты; отдельная БД с правом CREATE SCHEMA |

Неизвестный boolean, неправильный URL, Origin с путем и неположительные таймауты
останавливают запуск. URL/пароли, тело запроса, cookie и содержимое panic не логируются.
Для локальных значений используйте [.env.example](../.env.example).
Секреты для LLM/embedding пока не читаются: их переменные и провайдер определит BE2
при подключении настоящей модели, затем конфигурация будет дополнена.

## Локально с PostgreSQL на Windows или удаленной БД

Из корня репозитория, PowerShell:

```powershell
$env:DATABASE_ENABLED = 'true'
$env:DATABASE_URL = 'postgres://buddy:buddy_dev_only@localhost:5432/buddy?sslmode=disable'
$env:DEMO_MODE = 'true'
$env:ALLOWED_ORIGINS = 'http://localhost:5173,http://localhost:8080'
Set-Location backend
go run ./cmd/migrate
go run ./cmd/seed
go run ./cmd/server
```

Для удаленной БД укажите ее URL и подходящий sslmode; локальное `sslmode=disable`
из примера относится к тестовой машине. Миграции не создают саму БД/роль.
После запуска `/readyz` проверяет доступность БД. Если миграции не применены,
сервер останавливается с инструкцией выполнить migrate.

Отключение БД: `DATABASE_ENABLED=false` и `DEMO_MODE=true`. Тогда доступен только
HTTP-каркас. `/readyz` отвечает 503 и не выдает имитацию за рабочее хранилище.

## Миграции и транзакции

Инструмент первого этапа — встроенная команда `cmd/migrate`; SQL поставляется
в бинарнике через embed. Отдельную CLI устанавливать не нужно.

1. Владелец доменного модуля пишет SQL в `internal/platform/migrations/sql`.
2. BE1 перед объединением выделяет следующий номер после максимального в dev.
   Заняты `000001_platform.sql`, `000002_session.sql`, `000003_plan.sql`, `000004_chat.sql`.
   Следующий номер резервируется в PR.
3. Одинаковые номера, неизвестные имена, изменение примененного файла и добавление
   миграции перед уже примененной версией приводят к ошибке.
4. SQL выполняется в одной транзакции с журналом `buddy_schema_migrations`;
   PostgreSQL advisory lock сериализует параллельные команды. Повтор ничего не меняет.
5. Примененные файлы не редактируются/удаляются. Исправление — новая миграция вперед.
   DDL вне транзакции (например CREATE INDEX CONCURRENTLY) этим runner не поддерживается.

Репозитории внутри одной операции получают **тот же** `*sql.Tx` из
`database.WithinTx`. Ошибка callback и panic откатывают обе записи. Внешние вызовы
не входят в транзакцию. Черновик и связь с assistant координирует `drafts.Service`;
реальный SQL Store с проверкой владельцев появится с таблицами чата/заявок.
Миграции №9 создают пользователей/команды, серверные сессии и планы/шаги.
Demo seed — отдельная команда; повтор не изменяет прогресс и заявки.

## Тесты

Из `backend/`:

```powershell
go mod verify
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

Для интеграционных проверок можно использовать PostgreSQL без Docker или отдельный
контейнер, запускаемый **из корня репозитория**:

```powershell
docker compose --profile test up -d --wait db-test
$env:TEST_DATABASE_URL = 'postgres://buddy:buddy_test_only@localhost:5433/buddy_test?sslmode=disable'
Set-Location backend
go test -race -count=1 ./...
```

Тесты создают случайную изолированную схему и удаляют только ее; не удаляют данные
остальных схем. Проверяют параллельный/повторный migrate, checksum, откат DDL,
откат unit of work при ошибке/panic и успешный commit. Без TEST_DATABASE_URL — явный
SKIP, в CI отсутствие переменной считается ошибкой.

Отчет Go по всем прикладным пакетам:

```powershell
New-Item -ItemType Directory -Force ../tmp | Out-Null
go test '-coverpkg=./...' '-coverprofile=../tmp/coverage.out' ./...
go tool cover '-func=../tmp/coverage.out'
```

Это **statement coverage**, а не требуемая ТЗ метрика покрытия строк. Сегодняшний
CI сохраняет этот отчет, но не объявляет выполнение требования >80% строк.
Выбор измерителя строк и общий порог остаются частью завершения #15.

## Передача фронтенду

API: `http://localhost:8080`, операции сессий и плана — `/api/v1`.
Vite proxy: `/api` → `http://localhost:8080`, `changeOrigin: false`, чтобы сохранить
браузерный Origin. React обращается по относительным URL с cookie через fetch.
ALLOWED_ORIGINS должен содержать фактический адрес фронтенда (127.0.0.1 и localhost
считаются разными origin). Вход, me/logout и план реализованы в №9.
Команды запуска, шесть HTTP-примеров и границы demo-заглушки:
[сессии и план](../docs/backend/session-onboarding.md).
Диалоги, SSE и восстановление: [чат №10](../docs/backend/chat.md).
Инфраструктурные `/healthz` и `/readyz` не меняют бизнес-OpenAPI.

Для будущего SSE middleware не буферизует ответ и поддерживает Flush/ResponseController.
У сервера нет общего WriteTimeout; генерация ограничена 120 секундами в chat.
При reverse proxy отключить buffering и выставить read timeout >120 секунд;
heartbeat реализован с интервалом 15 секунд. До отправки заголовков panic
дает 500 ApiError, после начала ответа соединение обрывается без добавления JSON.

## CI и границы проверки

[Workflow](../.github/workflows/backend.yml): gofmt, go mod verify, go vet,
unit/PostgreSQL tests с race, statement coverage, сборка, валидатор контрактов,
Compose build/start, повтор migrate и перезапуск API. В Go-job тесты не требуют
личного .env или LLM-ключа. Контейнерный job дополнительно проверяет вход, изменение
плана, повтор seed, сохранение сессии и прогресса после пересоздания контейнеров.
[Прогон CI](https://github.com/Edyavna/AI-buddy/actions/runs/42664225153) подтвердил
настоящие SQL-тесты, контейнерный запуск, повтор миграций и сохранность данных после
пересоздания контейнеров без удаления тома. Тест
`TestModulesCanBeReplacedWithoutChangingHandler` проверяет подстановку разных
fake Agent/AccessProvider через один handler с реальной регистрацией httpapi.New.
Результат покрытия этого прогона — 85.3% statements, не line coverage.
