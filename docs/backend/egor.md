# Егор: корпоративная имитация

Рабочее задание принято Лёшей 9 ноября 2026. Твоя issue — #13.
Начни с [interfaces.md](interfaces.md) и [architecture.md](architecture.md).
Код в backend/internal/integrations, служебная команда backend/cmd/demo-control.
Твои данные находятся в demo_external, внутренние заявки/confirm/план принадлежат Лёше.

## Первая поставка

Реализуй ports.TaskProvider и ports.AccessProvider. Для MVP FirstTask возвращает одну
первую задачу в устойчивом порядке. Это принятое сужение ListMine из issue.
Задача имеет ID, название, описание, ресурс и при наличии URL; пользователь/команда
берутся из identity.Require(ctx). Общие ID: intern_demo_1, team_demo_backend,
repo_demo_backend, permission=read. Pending не означает выданного доступа.

Начни с FirstTask и Check на синтетических данных. Дай тестируемый конструктор
с инъекцией зависимостей; не запускай отдельный сервер. В тестах используй
testkit.Context. Рома принимает только read-only AccessChecker; worker Лёши —
AccessProvisioner. Один твой модуль может реализовывать оба интерфейса.

## Долговечная имитация

Подготовь миграции и идемпотентный seed для demo_external.tasks/resources/
access_requests/grants. Номер миграции выделяет Лёша перед merge. Seed не стирает
заявки/решения/права обычным запуском. Внешняя запись не имеет FK к внутренней заявке.
Ссылка request_id хранится как уникальный client_reference.

Submit одной собственной транзакцией создает pending и ExternalID. Повтор идентичного
payload после рестарта/конкурентного запроса возвращает тот же номер; другой payload
под тем же ключом — ErrConflict. Владелец проверяется до раскрытия записи.
Lookup по request_id возвращает актуальный pending/granted/rejected либо достоверный
ErrNotFound. Сетевой/временный сбой чтения возвращает ErrUnavailable.

Check проверяет grant, не наличие pending заявки. При approve выдача grant и смена
статуса фиксируются вместе. Повтор решения безопасен, противоположное конечному
решение отклоняется. Доступы и задачи чужой команды недоступны даже по известному ID.

## Demo-control

Предоставь CLI с документированными командами list/count/approve/reject и управлением
сбоями для конкретного request_id. Требуется DEMO_MODE=true. У CLI отдельный служебный
путь: он не подделывает стажерский Principal и не включается в браузерный API.
Стажер не получает возможность одобрять себе доступ. Интерфейс CLI остается
на русском, синтетичность данных обозначается явно.

Воспроизводимые сбои:

| Режим | Результат |
|---|---|
| Отказ до записи | failed без ExternalID и с безопасной причиной |
| Timeout после commit | ErrOutcomeUnknown, запись pending существует |
| Временная ошибка Lookup | ErrUnavailable, прежний статус сохраняется |
| Повтор Submit | Один номер и одна запись |

Timeout после записи не имитируется случайным sleep: тест/CLI явно выбирает этот
сценарий. Поздний первый Submit и повтор второго не должны создавать дубль.
GetRequestStatus из текста issue покрывается Lookup: он возвращает текущий статус,
отдельный метод по external_id для MVP не нужен.

## Проверки и передача

Три небольших PR: Task/Check, долговечные Submit/Lookup, CLI+сбои+README.
Проверь два конкурентных Submit, смену payload, restart, timeout после commit,
Lookup после timeout, чужого пользователя/команду, pending без grant и granted с grant.
Дай Лёше воспроизводимый путь create → pending → approve → granted и счетчик записей.
В README отдели имитируемые сервисы от настоящих, укажи будущую замену адаптера.
Внутренние draft_version/revision, confirm, jobs и изменение плана не дублируй.
