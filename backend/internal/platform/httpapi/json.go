package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

type Error struct {
	Status      int               `json:"-"`
	Code        string            `json:"code"`
	Message     string            `json:"message"`
	TraceID     string            `json:"trace_id"`
	FieldErrors map[string]string `json:"field_errors,omitempty"`
}

func (e *Error) Error() string { return e.Code }

// DecodeJSON distinguishes broken JSON (400) from shape/field violations (422).
// Semantic validation belongs to each handler and returns ValidationError.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) *Error {
	return DecodeJSONLimit(w, r, dst, 1<<20)
}

func DecodeJSONLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) *Error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return badRequest("Ожидается application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return badRequest("Некорректный JSON или превышен размер запроса")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return badRequest("Ожидается один JSON-объект")
	}
	if len(raw) == 0 || raw[0] != '{' {
		return ValidationError(nil)
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if err := strict.Decode(dst); err != nil {
		return ValidationError(nil)
	}
	return nil
}

func ValidationError(fields map[string]string) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_ERROR", Message: "Проверьте поля запроса", FieldErrors: fields}
}
func badRequest(message string) *Error {
	return &Error{Status: 400, Code: "BAD_REQUEST", Message: message}
}
func WriteError(w http.ResponseWriter, r *http.Request, err *Error) {
	copy := *err
	copy.TraceID = TraceID(r.Context())
	WriteJSON(w, copy.Status, struct {
		Error Error `json:"error"`
	}{copy})
}
func WriteJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		panic("JSON response cannot be encoded")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n')) // client disconnect needs no second response
}
