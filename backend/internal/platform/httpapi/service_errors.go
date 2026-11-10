package httpapi

import (
	"errors"
	"net/http"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

// WriteServiceError never exposes SQL, credentials or provider error text.
func WriteServiceError(w http.ResponseWriter, r *http.Request, err error) {
	result := &Error{Status: 500, Code: "INTERNAL_ERROR", Message: "Внутренняя ошибка"}
	switch {
	case errors.Is(err, identity.ErrUnauthenticated):
		result = &Error{Status: 401, Code: "UNAUTHENTICATED", Message: "Войдите в систему"}
	case errors.Is(err, ports.ErrForbidden):
		result = &Error{Status: 403, Code: "ROLE_FORBIDDEN", Message: "Операция доступна только стажеру"}
	case errors.Is(err, ports.ErrNotFound):
		result = &Error{Status: 404, Code: "NOT_FOUND", Message: "Объект не найден"}
	case errors.Is(err, ports.ErrUnavailable):
		result = &Error{Status: 503, Code: "SERVICE_UNAVAILABLE", Message: "Сервис временно недоступен"}
	}
	WriteError(w, r, result)
}
