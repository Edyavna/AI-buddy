# Рома: агент и знания

Рабочее задание принято Лёшей 9 ноября 2026. Твои issues — #11 и #12.
Начни с [interfaces.md](interfaces.md), [architecture.md](architecture.md) и
[SDD](../sdd/access-onboarding.md). Не жди настоящего чата/confirm: используй testkit.

## Область изменений

backend/internal/agent, backend/internal/knowledge, документы/fixtures твоего модуля,
docs/sdd и тесты. Один существующий Go module, общий slog/httpapi/identity.
Новый обработчик GET document принадлежит тебе; регистрацию в app делает Лёша.
Миграцию knowledge согласуй по следующему номеру перед merge. go.mod/go.sum,
.env.example и app/Compose передаются Лёше как конкретный перечень изменений.

## Первая поставка: работающий Run без сети

Создай Agent, реализующий ports.Agent. Его зависимости: ports.Knowledge,
TaskProvider, AccessChecker, DraftService и RequestReader плюс твой внутренний LLM
интерфейс с fake. Не принимай AccessProvider/AccessProvisioner: Submit агенту недоступен.

Fake LLM детерминированно выбирает task → search/read → check → prepare, после чего
формирует ответ. Prepare через DraftService получает сохраненный Draft и вызывает
emit(draft.saved). Последовательность callbacks, ошибка emit, отмена и частичный
AgentResult проверяются тестами. Эта поставка компилируется без ключей и PostgreSQL.
README показывает один вызов Run. Лёша подключает этот Agent к будущему chat service.

## Знания

Подготовь 8–12 коротких синтетических Markdown-документов. doc_local_setup и
team_demo_backend сохраняют согласованные IDs; добавь недоступный документ другой
команды для проверки ACL. Каждый материал явно обозначен как тестовый.

Реализуй importer с hash и идемпотентным обновлением; PostgreSQL pgvector(1024),
Jina jina-embeddings-v3 с retrieval.passage/query. HTTP-клиент и fake Embedder внутри
knowledge. Права фильтруются в SQL до LIMIT, повторно на Read и GET document.
Read возвращает реальный updated_at. Общие лимиты см. interfaces.md.

Заранее вычисленный индекс для demo допустим, подмена query embedding фейком в live
режиме — нет. Ошибку embedding отличай от пустого поиска. Набор минимум из 10 вопросов
проверяет синонимы, отсутствие ответа, чужую команду, повтор импорта и настоящие IDs.
Exact cosine search достаточно для объема MVP; HNSW не добавляй в первую реализацию.

## Живой агент

DeepSeek-клиент читает LLM_BASE_URL/LLM_MODEL/LLM_API_KEY. Начальная модель и embedding
конфигурация зафиксированы в architecture.md. Реализуй 6 tools из таблицы там;
аргументы типизированы, неизвестные поля запрещены. IDs/Principal привязывает Go.
Неизвестный tool и невалидные аргументы не выполняются. Неоднозначный ресурс требует
уточнения. Повтор уже submitting/pending возвращает состояние через RequestReader.

Источники строятся только из прочитанных материалов. Tool-ошибки и финальный ответ
не содержат ключей, сырых payload или внутренних рассуждений. Run: 120 секунд,
8 раундов, 16 tool calls, ошибка emit останавливает дальнейшие действия. Уже созданный
draft при сбое LLM сохраняется и возвращается в частичном AgentResult.

## Передача и приемка

Три небольших PR: Run+SDD+fake, knowledge+import, настоящий LLM+tools. В каждом —
команды тестов, свой README, изменения env/миграций и явные live-ограничения.
Поставь конкретные тесты по сценариям SDD; API-ключи не нужны для unit-тестов.
Отдельный live-прогон записывает модель/провайдер и реальный результат без ключа.
Проверка HTTP/SSE и настоящего SQL DraftService выполняется с Лёшей на этапе C/D.
Работа агента без внешнего confirm инструмента — обязательный критерий приемки.
