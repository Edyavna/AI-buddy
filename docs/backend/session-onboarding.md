# Сессии и план — №9

Шесть операций из OpenAPI v0.2 реализованы в Go. Настоящие сессии и прогресс
хранятся в PostgreSQL; React подключает их в №4. Чат/SSE и отправка заявок сюда
не входят. Текущие экраны React остаются демонстрационными до работы Валеры.

## Запуск и demo seed

Из корня репозитория с Docker:

```powershell
Copy-Item .env.example .env
docker compose up --build --wait api
docker compose --profile demo run --rm seed
```

Без Docker — установите переменные процесса из `.env.example`, затем из `backend/`:

```powershell
go run ./cmd/migrate
go run ./cmd/seed
go run ./cmd/server
```

Seed требует DEMO_MODE=true и включенную БД. Он вставляет отсутствующие данные,
не сбрасывает статусы, сессии или заявки. GET/login не создают планы.
Compose не запускает seed автоматически: отдельная команда позволяет явно выбрать
демонстрационные данные и не ломает запуск вне DEMO_MODE.

| account_id | Роль | Команда | Шаг |
|---|---|---|---|
| intern_demo_1 | intern | team_demo_backend | step_local_setup |
| intern_demo_2 | intern | team_demo_backend | step_local_setup_2 |
| intern_demo_other | intern | team_demo_frontend | step_local_setup_other |
| mentor_demo_1 | mentor | team_demo_backend | Нет бизнес-доступа |

Первый план использует ID из scenario.json. Его начальное состояние — in_progress,
request_ids=[], progress_percent=0. Пример scenario.json показывает более поздний
момент с уже существующим draft; seed не создает фиктивную заявку.

## Вызовы для Валеры

PowerShell, API на http://localhost:8080:

```powershell
$apiBase = 'http://localhost:8080/api/v1'
$apiHeaders = @{ Origin = 'http://localhost:8080' }
Invoke-RestMethod "$apiBase/demo/accounts"
Invoke-RestMethod "$apiBase/demo/session" -Method Post -Headers $apiHeaders `
  -ContentType 'application/json' -Body '{"account_id":"intern_demo_1"}' `
  -SessionVariable buddyWebSession
Invoke-RestMethod "$apiBase/me" -WebSession $buddyWebSession
Invoke-RestMethod "$apiBase/onboarding/plan" -WebSession $buddyWebSession
Invoke-RestMethod "$apiBase/onboarding/steps/step_local_setup" -Method Patch `
  -Headers $apiHeaders -WebSession $buddyWebSession -ContentType 'application/json' `
  -Body '{"status":"not_started"}'
Invoke-WebRequest "$apiBase/session" -Method Delete `
  -Headers $apiHeaders -WebSession $buddyWebSession
```

React использует общий apiRequest, относительные адреса и cookie same-origin;
Vite proxy уже направляет /api в Go. Браузер сам выставляет Origin. Разрешенные
origin задаются ALLOWED_ORIGINS; localhost и 127.0.0.1 различаются.

После входа тело ответа — User, cookie устанавливается через Set-Cookie. После
PATCH возвращается весь Plan, включая серверные allowed_statuses и next_step_id.
Все обязательные массивы возвращаются как [], nullable-поля — null.
Наставник может получить me и выйти; план/PATCH возвращают 403 ROLE_FORBIDDEN.
Чужой шаг — 404 NOT_FOUND, отсутствие/истечение сессии — 401 UNAUTHENTICATED.
Недопустимый переход — 409 STEP_TRANSITION_FORBIDDEN; неверный status/лишние поля —
422 VALIDATION_ERROR; недоступная БД/провайдер — 503 SERVICE_UNAVAILABLE.
Origin проверяется для POST/PUT/PATCH/DELETE, отсутствие или чужое значение — 403.
Все ошибки имеют общий ApiError с trace_id, личные ответы — no-store.

## Сессии

Случайный токен находится только в cookie; sessions хранит SHA-256, user_id и
expires_at. Пользователь/команда/роль восстанавливаются из БД на каждый запрос.
Смена аккаунта атомарно отзывает предъявленную старую сессию. Неудачный вход не
отзывает прежнюю. Другие устройства сохраняют свои сессии. Выход повторяемый,
возвращает пустой 204 и удаляет серверную запись и cookie.

SESSION_TTL по умолчанию 24h; допустимо 1s..8760h. Cookie HttpOnly, SameSite=Lax,
Path=/, с тем же сроком. При прямом TLS Secure включается автоматически; при
завершении HTTPS на доверенном прокси задайте SESSION_COOKIE_SECURE=true.
X-Forwarded-Proto от произвольного клиента не считается доказательством HTTPS.
DEMO_MODE=false исключает маршруты accounts/session, но me/logout и проверка
уже созданной сессии остаются доступны. Корпоративный вход — будущая интеграция.

## Правила плана и подключение №13/№14

Прогресс считается по равноценным шагам: floor(100*completed/total), пустой план — 0.
next_step_id — первый незавершенный неблокированный шаг, иначе null. Зависимости
шагов хранятся на сервере; незавершенная предпосылка блокирует последующий шаг.
Уже завершенный шаг сохраняет факт выполнения при повторном открытии предыдущего.

Для шага с политикой intern PATCH меняет рабочий статус или отметку стажёра;
completed дополнительно требует необходимого права через ports.AccessChecker.
allowed_statuses не содержит текущий статус, но его повтор разрешен без записи.
При blocked никакие пользовательские переходы не разрешены. Самостоятельно
выставить blocked нельзя. Выдача доступа не означает выполненную настройку.

В internal/app.NewHandler передается AccessChecker. В DEMO_MODE подключается
устойчивая имитация #13. Check читает demo_external.grants. Pending возвращает false,
после demo-control approve выдача права позволяет завершить шаг вручную.
Вне demo отсутствующая реализация дает 503.

Для №14 подготовлен `onboarding.ApplyRequestStateTx(ctx, tx, owner, stepID, request)`.
request — ports.RequestState с RequestID, Resource.ID/Permission, Status и при
наличии совпадающим StepID. owner — проверенный Principal владельца операции.
Метод не вызывает внешние сервисы, не делает commit и не проверяет переходы самой
заявки: request-service отвечает за права, актуальную версию/статус и идемпотентность.
Он передает тот же *sql.Tx, где пишет заявку и job, и первым блокирует родительский
план (до блокировки строки заявки). Для этого доступен LockPlanTx.

Ссылка draft не блокирует шаг. submitting/pending блокируют; rejected/failed
сохраняют блокировку, пока не появился granted для того же ресурса и права.
Другой ожидающий ресурс продолжает блокировать шаг. После снятия всех блокировок
granted снимает блокировку и восстанавливает статус по сохранённым отметкам.
Идентификаторы всех связанных заявок возвращаются в request_ids.

GET читает согласованный SQL snapshot под общей блокировкой плана. Проверки
доступа идут вне SQL-транзакции. PATCH берет исключительную блокировку и сравнивает
revision с проверенным snapshot. Если заявка успела заблокировать шаг, сервер
повторяет проверку и возвращает 409. После трех конфликтов — 503; ложного успеха
нет. Все писатели плана обязаны соблюдать эту блокировку и увеличивать revision.

## Чекпоинты №42 и каталог №44

Оба PUT подтверждений используют [контракт чекпоинтов](../../contracts/behavior.md).
При изменении отметки наставника `mentor.Store.AssignedInternTx` удерживает строки
назначения и пользователей до commit. Стажёр также проходит проверку роли под
блокировкой пользователя. Затем обработчик блокирует план и сохраняет шаг с отметкой
в одной транзакции. Если нужен AccessChecker, вызов идёт вне транзакции, а перед
записью сервер заново проверяет право на план и его revision.

Миграция 000012 создаёт активную отметку стажёра для каждого старого completed.
В старой схеме время выполнения не хранилось, поэтому backfill использует время
миграции. Статусы, ID шагов и связи заявок и чата остаются в существующих таблицах.

Загрузчик каталога может записывать `template_key`, `period`, `completion_policy`,
`instructions` и JSON-массив `resources` в `onboarding_steps`. Уникальность
обеспечивает индекс `(plan_id, COALESCE(template_key, id))`. При обновлении каталога
блокируйте план через `onboarding.LockPlanTx` перед изменением шагов. Сохраняйте
ID, статусы и отметки существующих шагов, увеличивайте `onboarding_plans.revision`
в той же транзакции при изменении плана.

## Проверка

Из backend, с TEST_DATABASE_URL на отдельную тестовую БД:

```powershell
go mod verify
go vet ./...
go test -race -count=1 -timeout=2m ./...
go build ./...
```

PostgreSQL-тесты создают изолированные базы данных (требуется CREATEDB). Проверяют серверные сессии, истечение,
смену аккаунта, mentor, чужой шаг в той же и другой команде, повтор PATCH/seed,
rollback связи/блокировки, несколько ресурсов, сохранение состояния и гонку
подтверждения со старой кнопкой completed. Без БД локально — явный SKIP; в CI БД
обязательна. Unit-тесты проверяют вычисление плана и ошибки без внешних сервисов.

Для проверки настоящего перезапуска из корня (Python 3, только stdlib):

```powershell
python scripts/smoke_session_onboarding.py prepare
docker compose restart api
docker compose --profile demo run --rm seed
python scripts/smoke_session_onboarding.py verify
```

Скрипт сохраняет demo-cookie только во временном tmp-файле и удаляет его после
успеха. CI выполняет этот сценарий с пересозданием контейнеров без удаления тома.
Это приемка №9; реальные React, заявки и адаптер №13 проверяются отдельной интеграцией.

### Локальная приемка 10 ноября 2026

- Go: зависимости проверены, vet/build и тесты с race detector прошли.
- PostgreSQL 17.6: SQL-тесты выполнены на настоящей БД без пропуска, включая
  изоляцию пользователей, зависимости шагов, rollback и конкурентную блокировку.
- Отдельный Go-процесс: prepare → остановка/новый запуск → повтор seed → verify
  прошли; сессия и прогресс сохранились, logout отозвал cookie.
- OpenAPI/fixtures/SSE/локальные ссылки — PASS; синтаксис YAML Compose/workflow — PASS.
- Покрытие всех Go-пакетов — 85.7% statements, не метрика строк из ТЗ.
- Docker на этой машине отсутствует: контейнерный прогон нового workflow локально
  не выполнен. Живая интеграция с React и настоящим AccessChecker не заявляется.
