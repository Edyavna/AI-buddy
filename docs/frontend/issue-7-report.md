# Отчёт по issue №7 — первая поставка (черновик + DraftService)

Дата: 2026-11-09. Ветка: `feature/issue-7-draft-card-integration` (от `dev`).

Согласно комментарию тимлида задача разбита на две поставки. Этот отчёт — по
первой: **сохранение черновика и DraftService** — «Никита проверяет, что в чате
появляется карточка с ID сохранённого черновика». Вторая поставка (confirm,
фоновая отправка, обновление плана на Go) остаётся за Лёшей (issue №14); до её
готовности фронт работает против согласованной заглушки — stateful MSW-имитации
(`src/mocks/handlers/fe2.ts`), построенной на каноническом `contracts/examples/scenario.json`.

## Что проверено

| Проверка (issue №7) | Результат | Где |
|---|---|---|
| Карточка с ID сохранённого черновика появляется в чате | ✅ | `sources-requests.test.tsx`, e2e, скриншот `chat-draft-*.png` |
| Отсутствие внешней заявки до подтверждения | ✅ | в e2e: внешний номер не показан, confirm не вызывался |
| Ровно одно создание, reload и сохранение external_id/status | ✅ | e2e: счётчик confirm == 1 после remount, `Номер: ACCESS-1001` сохраняется |
| granted и обновление плана, завершение шага пользователем | ✅ | e2e: поллинг submitting → pending → granted, прогресс 100% после «Отметить выполненным» |
| rejected, DRAFT_CHANGED, обрыв SSE, неизвестный исход confirm | ✅ | unit: `access-requests.test.tsx` (409/сеть/polling), `stream.test.ts`, `recovery_test.go` на бэкенде |
| 401/404 и сбои | ✅ | unit: `router.integration.test.tsx`, `access-requests.test.tsx`, `ChatPage.test.tsx` |
| 360 / 768 / 1440 px, клавиатура и фокус | ✅ | скриншоты `screenshots/`, фокус-проверки в `access-revocation.test.tsx` |
| generate:api, typecheck, lint, test:coverage, build, test:e2e | ✅ | команды ниже, покрытие lines **95.67%** (порог 81%) |
| Реальный сценарий с Go/LLM | ⏳ ждёт вторую поставку бэкенда | см. «Ограничения» |

## Команды проверки (воспроизводимые)

```sh
cd frontend
npm ci
npm run generate:api   # диффа с закоммиченными типами нет
npm run typecheck      # ✅
npm run lint           # ✅
npm run test:coverage  # ✅ 29 файлов, 168 тестов; lines 95.67% (порог 81%)
npm run build          # ✅
npm run test:e2e       # ✅ e2e/issue-7-scenario.e2e.test.tsx
```

Отчёты покрытия (text/HTML/LCOV) сохраняются в `frontend/coverage/`
(`index.html`, `lcov-report/`, `lcov.info`); пороги заданы в `vitest.config.ts`.

Сквозной e2e-сценарий (`npm run test:e2e`, `vitest.e2e.config.ts` + `e2e/`)
проходит весь путь приёмки `docs/frontend/acceptance.md`: вход тестовым
стажером → план → «Спросить помощника» без автоотправки → отправка вопроса,
источник, черновик без внешнего номера → один confirm → reload с сохранением
номера → granted и разблокировка шага → «Отметить выполненным» → прогресс 100%.

## Скриншоты (dev-сервер с MSW-имитацией)

| Файл | Что |
|---|---|
| `screenshots/plan-1440.png`, `plan-768.png`, `plan-360.png` | План адаптации с карточкой черновика в шаге |
| `screenshots/chat-draft-1440.png` | Чат с карточкой черновика (ID `request_demo_1`, кнопка «Подтвердить отправку», номера нет) |
| `screenshots/chat-granted-1440.png` | Тот же чат после confirm и сверки: «Доступ выдан», «Номер: ACCESS-1001» |
| `screenshots/chat-draft-768.png` | Чат на планшетной ширине |

## Ограничения и известные замечания

- **Вторая поставка Лёши (Go) не реализована**: в бэкенде нет пакета
  `accessrequests`, обработчиков confirm и worker'а. До их появления
  confirm/pending/granted обслуживает согласованная заглушка (MSW-имитация).
  DraftService (`backend/internal/drafts`) готов и покрыт тестами — поставка №1
  бэкенда выполнена.
- **Реальный сценарий с Go/LLM** из приёмки issue №7 не записан — ждёт вторую
  поставку; mock-тесты его по договорённости не заменяют.
- **Живой браузер, скрытая вкладка**: fetch confirm в фоновой вкладке может не
  завершаться, и тогда inflight-дедупликация блокирует повторные нажатия до
  перезагрузки страницы. В активной вкладке полный цикл подтверждён вручную
  (см. `chat-granted-1440.png`). Поведение поллинга при скрытой вкладке
  покрыто unit-тестом «skips polls while the tab is hidden».
- Покрытие измерено только по frontend `src`; итоговый охват проекта
  согласуется с бэкендом после второй поставки.
