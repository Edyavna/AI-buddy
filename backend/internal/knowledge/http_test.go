package knowledge

import (
	"context"
	"encoding/json"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
	"net/http/httptest"
	"testing"
)

func TestDocumentHandler(t *testing.T) {
	for _, tc := range []struct {
		name, id      string
		authenticated bool
		status        int
	}{
		{"accessible", "doc_local_setup", true, 200}, {"hidden", "doc_other_team", true, 404}, {"no session", "doc_local_setup", false, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.authenticated {
				ctx = testkit.Context(ctx)
			}
			r := httptest.NewRequest("GET", "/api/v1/documents/"+tc.id, nil).WithContext(ctx)
			r.SetPathValue("document_id", tc.id)
			w := httptest.NewRecorder()
			Handler(testkit.Knowledge{})(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d", w.Code)
			}
			if tc.status == 200 {
				var body map[string]any
				if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil {
					t.Fatal(e)
				}
				for _, field := range []string{"id", "title", "content", "updated_at"} {
					if body[field] == nil {
						t.Fatal("missing", field)
					}
				}
				if len(body) != 4 {
					t.Fatal(body)
				}
			}
		})
	}
}
