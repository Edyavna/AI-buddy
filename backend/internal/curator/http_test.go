package curator

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDirectHandlersRequireAuthenticatedPrincipal(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			request := httptest.NewRequest(method, "/", nil)
			response := httptest.NewRecorder()
			store := Store{}
			if method == http.MethodGet {
				store.list(response, request)
			} else {
				store.mutate(response, request)
			}
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("wanted 401, got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}
