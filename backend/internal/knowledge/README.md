# Knowledge (#11)

Нормализованный Document отделён от LoadMarkdown. manifest.json задаёт устойчивые ID,
источник, дату и ACL; содержимое читается из Markdown. Данные синтетические.
Права: совпадение команды И пустой список Users (вся команда) либо UserID в Users.
SQL фильтрует ACL до LIMIT 5. Read/Handler повторяют проверку, чужой ID даёт 404.
Фрагмент 1200 Unicode code points, шаг 1000; документ до 16000, запрос до 2000.

Jina jina-embeddings-v3: 1024 float, retrieval.passage/query. Exact cosine, без HNSW.
Порог Service.MinSimilarity по умолчанию 0.35, требует калибровки живым прогоном.
Ошибка провайдера даёт ErrUnavailable, никогда не подменяется пустым поиском.
Хеш включает заголовок и текст; ACL обновляется при неизменном хеше без API вызова.
Импорт запускается одним оператором последовательно; конкурентный импорт пока не поддержан.

Из backend:

```sh
go run ./cmd/knowledge-import -schema
# Владелец миграций применяет напечатанную схему к PostgreSQL с pgvector.
go run ./cmd/knowledge-import -dir ../demo/knowledge
go test ./internal/knowledge -v
```

DATABASE_URL, EMBEDDING_BASE_URL=https://api.jina.ai/v1 и EMBEDDING_API_KEY
читаются из окружения (файл .env сам по себе команда не загружает).
Схема передаётся Лёше через knowledge.Schema, номер общей миграции не занят.
Нужен образ PostgreSQL с установленным pgvector; текущий stock postgres его не содержит.
Необходимо согласовать образ и миграцию перед подключением app.

Регистрация Лёшей под существующим middleware сессии:

```go
k := &knowledge.Service{DB: db, Embedder: knowledge.JinaFromEnv()}
mux.Handle("GET /api/v1/documents/{document_id}", authHandler.Require(knowledge.Handler(k), true))
```

Фронт открывает GET /api/v1/documents/doc_local_setup,
ответ id/title/content/updated_at соответствует OpenAPI. Search endpoint отсутствует.

Проверки SQL: TEST_DATABASE_URL на отдельном disposable PostgreSQL с CREATEDB и
pgvector; TestPostgresACLAndImport проверяет повторный импорт, ACL-only отзыв,
Search/Read/GET, чужую команду, отличение сбоя от пустого поиска.
Без TEST_DATABASE_URL этот тест пропускается, качество embeddings он не измеряет.

Набор для живой оценки (ожидаемый документ):

| Вопрос | Ожидание |
|---|---|
| Как поднять проект? | doc_local_setup |
| Как настроить локальное окружение? | doc_local_setup |
| Что установить? | doc_tools |
| Как получить право чтения? | doc_access |
| pending значит уже можно клонировать? | doc_access |
| К кому идти за помощью? | doc_contacts |
| Как прогнать проверки? | doc_tests |
| Соединение с БД отклонено | doc_troubleshooting |
| Как оформить изменения на проверку? | doc_pull_request |
| Куда положить API токен? | doc_security |
| Какой обед в офисе? | нет ответа |
| analytics-private-canary от backend-команды | нет doc_other_team |

Живой отчёт пока не выполнен: нет настроенного PostgreSQL/ключа Jina.
