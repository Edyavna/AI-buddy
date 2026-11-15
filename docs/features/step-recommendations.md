# Следующий доступный шаг адаптации для стажёра (#55)

## Описание задачи

При входе на экран адаптации стажёру требуется чёткое понимание своего следующего шага:
какое действие сейчас доступно, к какому шагу оно относится, почему именно этот шаг предлагается,
и что делать, если текущий приоритетный шаг заблокирован или ожидает подтверждения наставника/доступов.

## Ключевые возможности

1. **Серверный движок рекомендаций**:
   - Автоматически рассчитывает следующий шаг по порядку плана, зависимостям (`prerequisites`), статусам, политикам завершения (`intern`, `mentor`, `both`) и подтверждениям (`confirmations`).
   - Различает 7 состояний:
     - `action_available` — доступно действие стажёра (`confirm`, `read_instructions`, `assistant`, `view_step`).
     - `waiting_mentor` — приоритетный шаг ожидает подтверждения наставника.
     - `waiting_access` — шаг ожидает согласования или выдачи доступов.
     - `blocked_dependencies` — шаг заблокирован зависимостями от предыдущих шагов.
     - `blocked` — шаг заблокирован.
     - `completed` — все шаги плана успешно завершены (100%).
     - `empty_plan` — план адаптации пуст.
   - **Выбор независимого шага**: если приоритетный шаг перешёл в ожидание наставника/доступов или ожидает подтверждения наставника при выполненной части стажёра (политика `both`), движок находит следующий независимый шаг, доступный для выполнения, а детали ожидания приоритетного шага изолирует в `waiting_step`.

2. **История и учёт времени блокировки шагов**:
   - Миграция `000026_step_blocking_history.sql`: колонка `blocked_at timestamptz` в `onboarding_steps` и таблица `onboarding_step_blocking_history` (с полями `id`, `step_id`, `blocked_reason`, `started_at`, `ended_at`).
   - При переходе шага в блокировку фиксируется время начала. При разблокировке или завершении интервал закрывается.
   - Расчёт `waiting_duration_seconds` в реальном времени с поддержкой легаси-шагов (`blocked_at: null`, `waiting_duration_seconds: null`).

3. **Интеграция с HR (#62)**:
   - Технические/рабочие шаги маршрутизируют помощь (`help_action`) наставнику (`direction: mentor`).
   - Кадровые/организационные шаги (оформление пропуска, документов, отпуска) маршрутизируют к HR (`direction: hr`).
   - Если у команды стажёра нет активного HR-специалиста, `available: false`, и интерфейс выводит понятное уведомление с рекомендацией обратиться к наставнику.

4. **Интерфейс на домашней странице `/onboarding`**:
   - `RecommendationCard` отображает бейдж состояния, название шага, период, причину рекомендации, целевое действие (`action_label`) и блок сведений об ожидающем приоритетном шаге.
   - Отображение длительности ожидания в человекочитаемом формате (`2 ч 15 мин`, `3 дн 4 ч`, `меньше минуты`).
   - Поддержка сохранения контекста: клик по рекомендации выбирает шаг и отражается в URL (`?stepId=...`), сохраняя выбранный шаг при перезагрузке страницы.

## Архитектура и контракты

### OpenAPI

- Модель `Recommendation` добавлена в схему `Plan` (`GET /onboarding/plan`, `PATCH /onboarding/steps/{id}`, `PUT /onboarding/steps/{id}/confirmation`).
- Выделен отдельный эндпоинт `GET /onboarding/recommendation` для легковесного опроса и синхронизации статуса рекомендации.

### Схема базы данных

```sql
ALTER TABLE onboarding_steps
    ADD COLUMN blocked_at timestamptz;

CREATE TABLE onboarding_step_blocking_history (
    id text PRIMARY KEY,
    step_id text NOT NULL REFERENCES onboarding_steps(id) ON DELETE CASCADE,
    blocked_reason text NOT NULL,
    started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    ended_at timestamptz
);

CREATE INDEX onboarding_step_blocking_history_step_idx
    ON onboarding_step_blocking_history (step_id, started_at);
```

## Исправления ревью PR #72

Ветка объединена с `dev bb6d4bb`: сохранены договорённости о встречах на странице плана,
архив диалогов и все существующие проверки CI. Миграция получила номер **000026**:
000024 уже выпущена, 000025 зарезервирована очередью действий в PR #81.
Регрессия `TestPostgresRecommendationUpgradePreservesReleasedArchive` проверяет обновление
выпущенной БД, сохранность архивированного диалога и повторный запуск миграций.

Доступ проверяется для приоритетного шага даже при статусе `in_progress`/`not_started`.
При его отсутствии рекомендация выбирает независимый доступный шаг. Ответ операции
подтверждения проверяет те же требования доступа, что обычное чтение плана.
Ожидание наставника после собственной отметки `both` отсчитывается от сохранённого
`confirmed_at`. Для старых блокировок без события и проектируемых зависимостей без
сохранённой даты таймер остаётся неизвестным (`null`); дата не выдумывается при чтении.

Ссылки в чат кодируют `stepId`. Помощь передаёт `direction=mentor|hr`, который сохраняется
при создании диалога и reload. Это предпочтение адресата в форме обращения: доступны
только направления, разрешённые текущим ответом #62. Пользователь может сменить адресата;
его локальный выбор не сбрасывается обновлением данных. Снимок чекпоинта передаётся
специалисту только после явного выбора и общего подтверждения контекста из #57.

Выбор карточки определяется URL; удаление `stepId` возвращает текущую рекомендацию.
Кнопка рекомендации выбирает карточку, прокручивает её в область просмотра и переводит
фокус. Название «Начать работу» больше не распознаётся как упоминание чата.
Кнопки используют контрастные цвета приложения, токены поверхностей и область нажатия
не менее 44 px. Существующие карусель, встречи и проверки прав сохранены.

Примеры содержат согласованные `next_step_id` и `recommendation.step_id`;
это дополнительно проверяет `scripts/check_contracts.py`. В OpenAPI и generated types
описано необязательное поле `Step.blocked_at`, уже возвращаемое сервером.

## Воспроизводимая браузерная проверка

Используйте **отдельную тестовую БД/Compose project**. SQL-фикстура специально меняет
синтетические демо-планы; к рабочей БД её применять нельзя. Команды из чистой среды
в bash, из корня проекта; PowerShell использует `$env:COMPOSE_FILE`.

```bash
export COMPOSE_FILE=compose.yaml:scripts/compose.hr-test.yaml
docker compose -p recommendation-55 up --build --wait --wait-timeout 180 frontend hr-fixture
docker compose -p recommendation-55 --profile demo run --rm seed
docker compose -p recommendation-55 exec -T db psql -U buddy -d buddy -v ON_ERROR_STOP=1 < scripts/recommendation-browser-fixture.sql
npm install --no-save --package-lock=false playwright-core@1.60.0
npx playwright-core install --with-deps chromium
node scripts/recommendation-browser.mjs
docker compose -p recommendation-55 restart api hr-fixture
docker compose -p recommendation-55 up -d --wait api hr-fixture
docker compose -p recommendation-55 --profile demo run --rm seed
node scripts/recommendation-browser.mjs --verify
```

Проверяются 360/768/1440 px, независимый/зависимый шаги, `both`, клавиатура и фокус,
выбранная карточка после reload, обновление по polling после отзыва наставника,
100% → отзыв отметки → новая рекомендация, переход в чат с адресатом, ручная смена
адресата, отправка HR-обращения с выбранным чекпоинтом, команда без HR и restart/reseed.
Артефакты: `tmp/recommendation-browser/`; в CI — `recommendation-browser`.

Можно задать `RECOMMENDATION_APP_URL`, `RECOMMENDATION_BROWSER`,
`RECOMMENDATION_PLAYWRIGHT_MODULE`. Локально использовались Chrome, Vite, Go и PostgreSQL;
CI повторяет сценарий через production Nginx SPA в Compose.
Внешний AI заменён детерминированной HTTP-фикстурой; живая модель не проверялась.
Сам алгоритм рекомендаций модель не вызывает.

## Проверки и покрытие

Frontend: `typecheck`, `lint`, `test:coverage`, `test:e2e`, `build`, `check:api`.
Backend с отдельной тестовой PostgreSQL: `go test -race -count=1 -timeout=2m -coverpkg=./... -coverprofile=coverage.out ./...`,
`go vet ./...`, `go build ./...`. В Docker Desktop локальный полный прогон использует
`-p 1 -timeout=5m`; штатные настройки CI не увеличены. Локальный frontend при параллельной
работе Docker запускался с `--maxWorkers=2`, без увеличения тайм-аутов.

Контракты: `python scripts/check_contracts.py` с `contracts/requirements-check.txt`.
Покрытие изменённых строк — `node scripts/hr-coverage.mjs origin/dev frontend|backend`;
Для Go из `backend/` сначала: `mkdir -p coverage` и
`go run ../scripts/go_line_coverage.go --lines coverage.out > coverage/lines.tsv`.
Generated, тесты и зависимости исключены штатными правилами.
Обе проверки строго **>90%** и общие пороги сохранены.
Отчёты по строкам: [frontend](issue55-coverage/frontend.txt), [backend](issue55-coverage/backend.txt).
Команды и результаты: [отчёт](issue55-coverage/results.txt), а также описание актуального PR #72.
