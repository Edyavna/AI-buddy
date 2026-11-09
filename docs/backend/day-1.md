# Первый день: каркас и передача

Дата: 9 ноября 2026. Область: #8 и минимальная часть #15.

## Реализовано

- Один Go module в backend; запуск HTTP, конфигурация, сигналы, graceful shutdown.
- PostgreSQL pool, отдельная команда встроенных SQL-миграций, журнал с checksum,
  транзакция и advisory lock, проверка примененной схемы при старте сервера.
- ApiError/trace_id, строгий JSON decoder, Origin allowlist, recovery без секретов.
- Principal в context; интерфейсы агента, знаний, задачи, доступа и черновиков.
- Координатор draft + связь с assistant в одной транзакции; тест отката при сбое связи.
- Тестовые реализации; компилируемый пример подключения агента.
- .env.example, Dockerfile, Compose с томом БД, readiness и отдельным migrate;
  изолированная тестовая БД; инструкции Windows без Docker и с PostgreSQL.
- Начальный CI для Go, контрактов и контейнерного запуска.

## Проверка на этой машине

Выполнено на Windows, Go 1.26.5:

- `gofmt -l .` — нет неотформатированных Go-файлов;
- `go mod verify`, `go vet ./...`, `go build ./...` — успешно;
- `go test -race -count=1 '-coverpkg=./...' '-coverprofile=../tmp/coverage.out' ./...` —
  успешно, PostgreSQL-тесты явно пропущены без TEST_DATABASE_URL;
- `go tool cover '-func=../tmp/coverage.out'` — **73.3% statements** по всем пакетам,
  включая entrypoints; это не метрика строк и не выполнение финального порога ТЗ;
- валидатор контрактов — PASS: OpenAPI 0.2.0, 15 операций, fixtures, SSE,
  негативные примеры и ссылки;
- синтаксис YAML Compose/workflow и локальные ссылки backend-документов проверены;
- собранный server.exe фактически запущен без БД: `/healthz` — 200,
  `/readyz` — ожидаемый 503 ApiError; процесс после проверки остановлен.

Docker/PostgreSQL на машине отсутствуют: контейнерный запуск и живые SQL-тесты
здесь не подтверждены. Проверка graceful shutdown выполнена отдельным HTTP-тестом.
Workflow содержит эти проверки для среды с Docker/PostgreSQL; добавление workflow
не является доказательством его успешного выполнения.

## Приемка участником с Docker

Из корня AI-buddy:

```powershell
Copy-Item .env.example .env
docker compose config --quiet
docker compose up --build --wait api
Invoke-RestMethod http://localhost:8080/healthz
Invoke-RestMethod http://localhost:8080/readyz
docker compose run --rm migrate
docker compose restart api
```

После перезапуска readyz должен снова отвечать 200. Запуск тестов PostgreSQL
описан в [backend/README.md](../../backend/README.md).

## Что остается открытым

1. Проверить сборку/запуск контейнеров и SQL-тесты в подходящей среде.
2. Согласовать [интерфейсы](interfaces.md) с Ромой и Егором; сейчас это предложение.
3. Передать ветку/PR и получить независимую проверку запуска по README.

План первого дня не означает реализацию всего API. Сессии/план (#9), чат/SSE (#10),
знания/агент/корпоративные адаптеры, SQL Store черновиков и confirm (#14), demo seed,
LLM-конфигурация и финальная метрика покрытия строк (#15) остаются следующими этапами.
Полную приемку #8 нельзя считать закрытой до проверки БД и подключения потребителями.
