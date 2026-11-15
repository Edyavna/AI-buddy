package agent

import "strings"

const role = "Я AI Buddy, помощник по адаптации стажёров в Авито. Я объясняю шаги плана, помогаю разобраться с доступами и подсказываю, как обратиться к наставнику или HR."
const demoRole = "Это прототип. Корпоративные данные и действия в этой среде могут быть демонстрационными."

func roleDescription(demo bool) string {
	if demo {
		return role + " " + demoRole
	}
	return role
}

// RoleAnswer handles standalone questions that need only trusted server facts.
// Questions with other clauses go through the ordinary conversation path.
func RoleAnswer(text string, demo bool) string {
	text = strings.ToLower(strings.Join(strings.Fields(text), " "))
	text = strings.TrimSpace(strings.TrimRight(text, "?!."))
	switch text {
	case "кто ты", "кто вы", "как тебя зовут", "что такое ai buddy",
		"чем ты помогаешь", "чем ты можешь помочь", "что ты умеешь", "для чего ты нужен",
		"в какой компании я прохожу адаптацию", "в какой компании я стажируюсь", "в какой компании проходит моя адаптация":
		return roleDescription(demo)
	default:
		return ""
	}
}
