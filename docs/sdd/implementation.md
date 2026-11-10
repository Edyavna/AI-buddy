# Реализация Ромы

До кода фиксируем сценарии S01–S09, S17–S20 из access-onboarding.md.
Given доверенная сессия, when модель вызывает tool, then Go проверяет аргументы
и права. Given чужой документ, then Search/Read/GET скрывают его. Given черновик
сохранён, when emit или LLM падает, then partial result содержит его ID.
Given текст «подтверждаю» или инструкция в документе, then отправка невозможна:
такого инструмента и зависимости у Agent нет. Given повтор tool, then цикл
останавливается. Given active pending, then возвращается запись без draft.saved.

SQL и live-проверки требуют отдельного окружения и не заменяются fake-прогоном.
