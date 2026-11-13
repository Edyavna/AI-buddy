package reviewprep

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// Поддерживается только публичный формат GitHub; иначе — явная ошибка валидации.
var prPathPattern = regexp.MustCompile(`^/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/pull/([0-9]+)/?$`)

const (
	maxURLLength  = 2048
	maxRepoLength = 200
	maxPRNumber   = 99999999
)

// ParsePRURL проверяет поддерживаемый адрес и возвращает repo "owner/name" и номер PR.
func ParsePRURL(raw string) (repo string, number int, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxURLLength {
		return "", 0, ports.ErrInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", 0, ports.ErrInvalid
	}
	if !strings.EqualFold(u.Hostname(), "github.com") {
		return "", 0, ports.ErrInvalid
	}
	parts := prPathPattern.FindStringSubmatch(u.Path)
	if parts == nil {
		return "", 0, ports.ErrInvalid
	}
	owner, name := parts[1], parts[2]
	if len(owner)+len(name)+1 > maxRepoLength {
		return "", 0, ports.ErrInvalid
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil || n < 1 || n > maxPRNumber {
		return "", 0, ports.ErrInvalid
	}
	return fmt.Sprintf("%s/%s", owner, name), n, nil
}
