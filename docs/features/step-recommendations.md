# Следующий доступный шаг адаптации для стажёра (#55)

## Описание задачи

При входе на экран адаптации стажёру требуется чёткое понимание своего следующего шага:
какое действие сейчас доступно, к какому шагу оно относится, почему именно этот шаг предлагается,
и что делать, если текущий приоритетный шаг заблокирован или ожидает подтверждения наставника/доступов.

## Ключевые возможности

1. **Серверный движок рекомендаций**:
   - Автоматически рассчитывает следующий шаг по порядку плана, зависимостям (`request_ids`), статусам, политикам завершения (`intern`, `mentor`, `both`) и подтверждениям (`confirmations`).
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
   - Миграция `000019_step_blocking_history.sql`: колонка `blocked_at timestamptz` в `onboarding_steps` и таблица `onboarding_step_blocking_history` (с полями `id`, `step_id`, `blocked_reason`, `started_at`, `ended_at`).
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

## Результаты тестирования и покрытие

- **Backend**:
  - `backend/internal/onboarding/recommendation_test.go`: юнит-тесты всех состояний, фильтрации независимых шагов, расчета времени ожидания, защиты от clock skew.
  - `backend/internal/app/session_plan_test.go`: интеграционные тесты для эндпоинта рекомендации и пустого плана.
  - Покрытие изменённых строк Go: **100%** (с набором тестов PostgreSQL в CI).
- **Frontend**:
  - `frontend/src/features/onboarding/recommendation.test.tsx`: 21 тест (компонент `RecommendationCard`, форматирование длительности `formatWaitingDuration`, интеграция с `PlanPage` и персистентность `?stepId=...`, клиент API).
  - Покрытие изменённых строк фронтенда: **100.00% (26/26)**.
- **Контракты**:
  - `python scripts/check_contracts.py`: **PASS** (94 operations, валидация примеров и схем).
