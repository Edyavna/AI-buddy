package knowledge

import (
	"sort"
	"strings"
	"unicode"
)

var groupStopWords = map[string]bool{"как": true, "где": true, "что": true, "для": true, "мне": true, "меня": true, "можно": true, "нужно": true, "пожалуйста": true, "помоги": true, "почему": true, "кто": true, "это": true, "или": true, "при": true, "мой": true, "моя": true, "the": true, "how": true, "can": true, "and": true, "какой": true, "какие": true}

func groupTokens(text string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ReplaceAll(strings.ToLower(text), "ё", "е"), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(word)) < 3 || groupStopWords[word] {
			continue
		}
		// Deliberately conservative stemming. Identifiers and short words stay exact.
		if len([]rune(word)) > 5 {
			for _, suffix := range []string{"иями", "ами", "ого", "ему", "ить", "ать", "ять", "ение", "ения", "ений", "ов", "ом", "ам", "ах", "ий", "ый", "ая", "ой", "ы", "а", "у", "е", "и"} {
				if strings.HasSuffix(word, suffix) {
					word = strings.TrimSuffix(word, suffix)
					break
				}
			}
		}
		out[word] = true
	}
	return out
}

// SimilarQuestions never grants visibility. Callers supply only authorized,
// same-direction/team open questions; the UI still requires explicit selection.
func SimilarQuestions(a, b string, sameCheckpoint bool) (float64, string) {
	left, right := groupTokens(a), groupTokens(b)
	common := []string{}
	for word := range left {
		if right[word] {
			common = append(common, word)
		}
	}
	union := len(left) + len(right) - len(common)
	if union == 0 {
		return 0, ""
	}
	score := float64(len(common)) / float64(union)
	if !(len(common) >= 2 && score >= 0.35 || sameCheckpoint && len(common) >= 1 && score >= 0.18) {
		return score, ""
	}
	sort.Strings(common)
	if len(common) > 4 {
		common = common[:4]
	}
	reason := "Общие слова темы: " + strings.Join(common, ", ") + "."
	if sameCheckpoint {
		reason += " Совпадает чекпоинт."
	}
	return score, reason
}
