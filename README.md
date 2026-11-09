# AI-buddy

Прототип ИИ-помощника для адаптации стажеров. Планируемый стек: Go, React + TypeScript,
PostgreSQL и внешний LLM API. Текущая версия содержит контракты и инструкции;
приложение еще не реализовано.

Основной сценарий: первая задача → инструкции команды → отсутствие доступа →
подтверждение заявки → сохраненный номер и статус → следующий шаг.

## Документы

- [Контракт React ↔ Go](contracts/README.md)
- [OpenAPI](contracts/openapi.yaml)
- [Поток чата](contracts/chat-events.md)
- [Начало работы фронтендеров](docs/frontend/README.md)
- [Задание FE1](docs/frontend/frontend-1.md)
- [Задание FE2](docs/frontend/frontend-2.md)
- [Стыки фронтенда](docs/frontend/integration.md)
- [Приемка и тестирование](docs/frontend/acceptance.md)

Исходное ТЗ находится в PDF в корне. Задания реализации ведутся в GitHub Issues.
Инструкция запуска приложения будет добавлена вместе с рабочим каркасом.
