package knowledge

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Edyavna/AI-buddy/backend/internal/auth"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/httpapi"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func (s Uploads) Register(mux *http.ServeMux, session auth.Handler) {
	mux.Handle("GET /api/v1/mentor/documents", session.Require(http.HandlerFunc(s.listHTTP), false))
	mux.Handle("POST /api/v1/mentor/documents", session.Require(http.HandlerFunc(s.createHTTP), false))
	mux.Handle("GET /api/v1/mentor/documents/{id}", session.Require(http.HandlerFunc(s.getHTTP), false))
	mux.Handle("POST /api/v1/mentor/documents/{id}/retry", session.Require(http.HandlerFunc(s.retryHTTP), false))
}

func (s Uploads) listHTTP(w http.ResponseWriter, r *http.Request) {
	items, err := s.List(r.Context())
	if err != nil {
		writeUploadError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, struct {
		Items []Upload `json:"items"`
	}{items})
}

func (s Uploads) getHTTP(w http.ResponseWriter, r *http.Request) {
	d, err := s.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeUploadError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, d)
}

func (s Uploads) retryHTTP(w http.ResponseWriter, r *http.Request) {
	d, err := s.Retry(r.Context(), r.PathValue("id"))
	if err != nil {
		writeUploadError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusAccepted, d)
}

func (s Uploads) createHTTP(w http.ResponseWriter, r *http.Request) {
	if _, err := mentorPrincipal(r.Context()); err != nil {
		writeUploadError(w, r, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxMarkdownBytes+16*1024)
	in, err := readUpload(r)
	if err != nil {
		writeUploadError(w, r, err)
		return
	}
	d, created, err := s.Create(r.Context(), in)
	if err != nil {
		writeUploadError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusAccepted
	}
	httpapi.WriteJSON(w, status, d)
}

func readUpload(r *http.Request) (UploadInput, error) {
	var in UploadInput
	reader, err := r.MultipartReader()
	if err != nil {
		return in, ports.ErrInvalid
	}
	fields := map[string]*string{"upload_id": &in.UploadID, "question_id": &in.QuestionID, "title": &in.Title, "file": &in.Content}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return in, ports.ErrInvalid
		}
		name := part.FormName()
		field, ok := fields[name]
		if !ok || (name == "file" && !strings.EqualFold(filepath.Ext(part.FileName()), ".md")) ||
			(name != "file" && part.FileName() != "") {
			return in, ports.ErrInvalid
		}
		limit := 1024
		if name == "file" {
			limit = MaxMarkdownBytes
		}
		data, err := io.ReadAll(io.LimitReader(part, int64(limit+1)))
		if err != nil || len(data) > limit {
			return in, ports.ErrInvalid
		}
		*field = string(data)
		delete(fields, name)
	}
	return in, nil
}

func writeUploadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrInvalid):
		httpapi.WriteError(w, r, httpapi.ValidationError(nil))
	case errors.Is(err, ports.ErrForbidden):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: "ROLE_FORBIDDEN", Message: "Операция доступна только наставнику"})
	case errors.Is(err, ports.ErrConflict):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "IDEMPOTENCY_CONFLICT", Message: "Этот идентификатор уже использован для другой загрузки"})
	case errors.Is(err, ErrAlreadyReady):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "DOCUMENT_ALREADY_READY", Message: "Документ уже проиндексирован"})
	default:
		httpapi.WriteServiceError(w, r, err)
	}
}
