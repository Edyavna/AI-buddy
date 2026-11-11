# Источники и заявки в чате — FE1 #6

Ветка `feature/fe1-chat-sources-requests` создана от dev на bfb5f19.
Маршруты, RequireAuth, общий QueryClient и API-контракт не менялись.

## Документы и Markdown

Публичные экспорты `features/documents`: `MarkdownContent` и
`DocumentDrawer({ documentId: string | null, onClose })`. Панель закрыта при null.
GET документа использует общий API-транспорт, AbortSignal, runtime-схему и
`queryKeys.document(id)`. Загрузка, ошибки формата/сети, повтор и 404 показаны
внутри общего Drawer FE2, без изменения URL и текста composer.
На ширине меньше 768px документ занимает всю ширину. Tab/Shift+Tab остаются
внутри панели, Escape закрывает её, фокус возвращается на кнопку источника.

Добавлены react-markdown и remark-gfm с закреплёнными версиями в едином lockfile.
Поддерживаются списки, таблицы, ссылки, inline-код, fenced-блоки и копирование
кода через Clipboard API с сообщением об успехе/ошибке. Raw HTML не исполняется.
Ссылки допускают http, https, mailto и относительные адреса; опасные схемы,
в том числе скрытые управляющими символами, и protocol-relative URL не становятся ссылками.
Ссылки открываются отдельно с noopener/noreferrer. Внешние изображения не
загружаются: выводится alt-текст. Пользовательские вопросы остаются plain text.

## Общая карточка FE2

`action.proposed` проверяется публичной `accessRequestSchema` FE2 с дополнительными
условиями draft/external_id=null. После проверки message ID и conversation ID
контроллер вызывает `acceptAccessRequest(queryClient, request)` и сохраняет связь
request_id с сообщением. Локальная копия схемы заявки из chat/schemas удалена.
Revision, защита от старых событий и обновление при равных конфликтующих версиях
остаются в общем механизме FE2.

ChatPage отображает настоящий `AccessRequestPanel` по каждому уникальному
`request_ids`. GET/confirm/polling, draft_version и внешний номер принадлежат
FE2; собственного confirm и второго кеша в чате нет. После reload источники
и карточки восстанавливаются из GET messages, старые SSE-события не нужны.
`onDiscuss` только фокусирует composer, не отправляет вопрос.

Источники выводятся по `Message.sources` (title/excerpt/document_id), номер
заявки — только компонентом FE2 из AccessRequest.external_id. Черновик не
показывается отправленной заявкой, подтверждение требует явного нажатия.

Прокрутка сохраняется при чтении истории выше конца. ResizeObserver следит
за изменением высоты Markdown/карточек: у конца список следует за контентом,
выше конца появляется кнопка «Новые сообщения». Текст потока не озвучивается
каждым токеном; сообщения статуса и ошибки доступны отдельно.

## Проверки и ограничения

Из frontend/: `npm run typecheck`, `npm run lint`, `npm run test:coverage`,
`npm run build`, `npm run check:api`.

Тесты проверяют безопасные URL/HTML, Markdown и Clipboard API, загрузку/404/повтор
документа, Tab/Shift+Tab/Escape и возврат фокуса. Интеграционные тесты используют
реальный AccessRequestPanel FE2: черновик до SSE-финала, отсутствие confirm до
нажатия, переданную draft_version, номер из HTTP, восстановление после remount,
дедупликацию карточек и фокус composer. Контроллер проверяется на старую revision,
конфликт равной revision и событие с чужим conversation_id. Изменение высоты
контента и прокрутка проверяются через моделируемый ResizeObserver.

Browser/CUA и запасной Windows Computer Use не запустились в этой среде:
`failed to write kernel assets` / `os error 3`. Поэтому живая визуальная приёмка
на 360/768/1440px не заявляется; responsive-стили и клавиатурное поведение
проверены кодом и component-тестами. MSW-проверки не доказывают сквозной путь с
реальными Go/LLM и корпоративным сервисом — он относится к общей приёмке #7.
