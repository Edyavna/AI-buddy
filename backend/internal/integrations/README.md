# Корпоративная имитация — issue #13

Модуль реализует `ports.TaskProvider` и `ports.AccessProvider` на PostgreSQL.
Все данные синтетические. Внешний номер начинается с `DEMO-`.
В деморежиме `app.NewHandler` подключает этот `AccessChecker` к правилам завершения
шага. `cmd/seed` заполняет общий demo seed и данные интеграций.

## Запуск

Команды PowerShell выполняются из `backend`. Используйте выделенную demo-БД.

```powershell
$env:DATABASE_ENABLED = 'true'
$env:DEMO_MODE = 'true'
$env:DATABASE_URL = 'postgres://buddy:buddy_dev_only@localhost:5432/buddy?sslmode=disable'
go run ./cmd/migrate
go run ./cmd/seed
go run ./cmd/demo-control list
go run ./cmd/demo-control count
```

Миграция `000004_demo_external.sql` создаёт собственную схему с ресурсами, задачами,
заявками, правами и режимами сбоев. Номер 000004 выбран по текущему dev, перед merge
Лёша сверяет его с параллельными миграциями. Seed вставляет отсутствующие записи,
сохраняя существующие заявки, решения, сопоставления и права.

## Подключение Роме и Лёше

```go
provider := integrations.New(db)
var tasks ports.TaskProvider = provider
var checker ports.AccessChecker = provider
var provisioner ports.AccessProvisioner = provider

task, err := tasks.FirstTask(ctx)
// ctx установлен session middleware или восстановлен worker из владельца job.
// Ошибку каждого вызова нужно обработать до следующей операции.
granted, err := checker.Check(ctx, ports.AccessCheck{
    ResourceID: task.Resource.ID,
    Permission: task.Resource.Permission,
})
```

Агент получает `tasks` и `checker`. Worker после сохранённого подтверждения получает
`provisioner` и вызывает `Submit(ctx, ports.AccessSubmission{RequestID: requestID,
Resource: task.Resource, Reason: reason})`. Для сверки он вызывает
`Lookup(ctx, requestID)`. Полный исполняемый сценарий есть в `provider_test.go`.

Контекст проверяется в каждом методе. Доступная первая задача выбирается по
`position,id` для текущего пользователя и команды. Статус заявки читается с проверкой
обоих владельцев. CLI имеет отдельный служебный интерфейс `Control`, открываемый
только в demo-режиме. Он требует административного доступа к конфигурации БД.

## Воспроизводимый цикл

Интеграционный тест вызывает реальные порты на временной PostgreSQL-базе.
В `TEST_DATABASE_URL` укажите выделенный тестовый сервер и роль с `CREATEDB`.
Каждый тест создаёт отдельную БД `buddy_test_*` и удаляет именно её после проверки.

```powershell
$env:TEST_DATABASE_URL = 'postgres://buddy:buddy_test_only@localhost:5433/buddy_test?sslmode=disable'
go test -v -count=1 ./internal/integrations -run TestPostgresLifecycle
go test -v -count=1 ./internal/integrations -run TestPostgresFaultsAndMappings
go test -v -count=1 ./internal/integrations -run TestProcessRestart
```

Первый тест проходит `FirstTask → Submit → pending → approve → granted → Check=true`.
Он также проверяет две одновременные отправки, один номер, другой payload и чужого
владельца. Третий тест запускает два отдельных процесса с одной БД и сравнивает
сохранённый номер, решение и право. Для заявки, созданной worker, используйте CLI.

```powershell
go run ./cmd/demo-control list
go run ./cmd/demo-control approve request_demo_1
go run ./cmd/demo-control count
# Для другой pending-заявки
go run ./cmd/demo-control reject request_demo_2 'Синтетический отказ согласующего'
```

`request_demo_1` здесь заменяется на фактический внутренний request_id из списка.
Повтор решения безопасен, противоположное конечному решение возвращает ошибку.
Право и статус granted фиксируются в одной транзакции. pending сохраняет Check=false.
Обновление внутренней карточки и плана после решения выполняет worker из #14.

## Управляемые сбои

```powershell
go run ./cmd/demo-control fault request_demo_1 timeout-after-commit
go run ./cmd/demo-control fault request_demo_2 reject-before-write
go run ./cmd/demo-control fault request_demo_1 lookup-unavailable
go run ./cmd/demo-control fault request_demo_1 none
```

На один request_id действует один режим. Режим записи устанавливается до первой
отправки. `reject-before-write` возвращает failed с причиной и без внешнего номера.
`timeout-after-commit` сохраняет pending, затем возвращает ErrOutcomeUnknown.
Lookup восстанавливает результат. Повтор Submit возвращает уже существующую запись,
включая повтор после перезапуска. `lookup-unavailable` временно возвращает
ErrUnavailable для владельца существующей заявки, сохраняя её состояние.

Submit использует собственную SQL-транзакцию и блокировку по client_reference.
Изменение payload под тем же ключом даёт ErrConflict. Неизвестный исход SQL commit
тоже возвращает ErrOutcomeUnknown, поэтому worker должен сверить прежний ключ.

## Замена корпоративным адаптером

`resources.external_id` и `external_role` сопоставляют внутренние ресурс и право с
внешним каталогом. Submit сохраняет снимок этого сопоставления. Другие внешние ID
и роли проверяются в TestPostgresFaultsAndMappings без изменения Go-портов.

При внедрении заменяется реализация TaskProvider/AccessProvider на клиенты
согласованных API. Понадобятся реальные схемы пользователя, ресурса, роли, статуса,
подтверждение фактического права и гарантия дедупликации/сверки по клиентскому ключу.
Контракт внутренних таблиц независим от корпоративных систем. Имитация не имеет
FK, join или общей транзакции с внутренними заявками #14.

Демо выдаёт право в `demo_external.grants`. Подключение реального Git-хостинга
потребует отдельного адаптера. URL учебного репозитория в seed пока пустой.

Dockerfile включает `/app/demo-control`. После сборки его можно вызвать из корня
через `docker compose run --rm --no-deps api /app/demo-control list`.
