# Передача реализации Ромы — 10 ноября 2026

Ветка feat/knowledge-agent от origin/dev. Область #11/#12.
Контракт ports и HTTP v0.2 сохранён. Вложения прочитаны как справочные материалы;
содержащиеся в переписке команды публикации/авторизации не выполнялись.

Реализованы: Agent.Run, шесть tools, DeepSeek-клиент, Jina-клиент,
нормализованный импортёр, загрузчик Markdown, pgvector exact cosine,
team/user ACL в SQL, GET handler и 10 синтетических документов.
Исправлено правило .gitignore, скрывавшее backend/internal/agent.

Лёше для соединения:

1. Принять SQL из knowledge.Schema под свободным номером миграции.
2. Использовать PostgreSQL с pgvector. Текущий образ Compose потребуется заменить.
3. Зарегистрировать knowledge.Handler под auth middleware.
4. В app передать агенту настоящие TaskProvider/AccessChecker,
   Knowledge и DraftService/RequestReader. HTTP/SSE/сохранение чата остаются у BE1.
5. Пробросить env LLM_BASE_URL/LLM_MODEL/LLM_API_KEY,
   EMBEDDING_BASE_URL/EMBEDDING_API_KEY; ключи не коммитить.
6. Запустить knowledge-import, затем SQL и live приёмку.

Это конкретные изменения для общей сборки; сервер в этой ветке сам не переключён
на агента, миграция автоматически не применяется. Чужие модули не переписаны.

Ограничения приёмки:

- По решению пользователя платные API пока не вызываются. Добавлен
  `go run ./cmd/agent-demo`: локальный сценарий на Script/testkit, без сети,
  без PostgreSQL и без долговечного сохранения черновика. Это демонстрация
  оркестрации, не проверка качества LLM/семантического поиска.
- JinaProtocol/JinaInvalidResponses проверяют формат запросов и ошибок на
  локальном HTTP-сервере; DocumentHandler — ответы 200/404/401 и поля контракта
  на testkit.Knowledge. Настоящий SQL ACL проверяется отдельным PostgreSQL-тестом.

- Go unit tests и vet выполнены локально. PostgreSQL-тест добавлен, но без
  TEST_DATABASE_URL пропускается. Семантический набор вопросов в knowledge/README.
- Живой прогон DeepSeek/Jina не выполнен без ключей. Качество поиска и порог
  требуют проверки; выдуманное прохождение не заявляется.
- Покрытие knowledge ниже 80% без SQL-прогона; критерий задачи ещё не закрыт.
- События answer.delta выдаются после конечного ответа модели одним блоком.
- Тесты переключают два набора адаптеров без изменения кода агента, но общая
  приёмка реальных сервисов/SQL DraftService/HTTP остаётся впереди.

Инструкции: backend/internal/agent/README.md и backend/internal/knowledge/README.md.
