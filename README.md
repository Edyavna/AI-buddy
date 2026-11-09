# AI-buddy

Прототип ИИ-помощника для адаптации стажеров. Планируемый стек: Go, React + TypeScript,
PostgreSQL и внешний LLM API. Реализована frontend-основа API, кеша и тестовых
имитаций (#2); пользовательские экраны и Go-сервис ещё разрабатываются.

Основной сценарий: первая задача → инструкции команды → отсутствие доступа →
подтверждение заявки → сохраненный номер и статус → следующий шаг.

## Документы

- [Контракт React ↔ Go](contracts/README.md)
- [OpenAPI](contracts/openapi.yaml)
- [Поток чата](contracts/chat-events.md)
- [Поведение API и правила повторов](contracts/behavior.md)
- [Общие примеры JSON](contracts/examples/scenario.json)
- [Начало работы фронтендеров](docs/frontend/README.md)
- [Задание FE1](docs/frontend/frontend-1.md)
- [Задание FE2](docs/frontend/frontend-2.md)
- [Стыки фронтенда](docs/frontend/integration.md)
- [Приемка и тестирование](docs/frontend/acceptance.md)
- [Запуск проверок API-основы и подключение FE2](frontend/README.md)

Исходное ТЗ находится в PDF в корне. Задания реализации ведутся в GitHub Issues.
Инструкция запуска приложения будет добавлена вместе с рабочим каркасом.
Интеграционная ветка — `dev`; task-ветки создаются от неё и объединяются в неё.
Готовая версия затем объединяется в `main`.
