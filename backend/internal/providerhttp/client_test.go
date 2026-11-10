package providerhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPost(t *testing.T) {
	for _, body := range []string{`{"ok":true}`, `broken`, strings.Repeat("x", (2<<20)+1)} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test" {
				t.Error("missing credential")
			}
			w.Write([]byte(body))
		}))
		var out map[string]any
		e := (Client{BaseURL: s.URL, Key: "test"}).Post(context.Background(), "/test", map[string]int{"x": 1}, &out)
		if (e == nil) != (body == `{"ok":true}`) {
			t.Fatal(e)
		}
		s.Close()
	}
}
func TestFailures(t *testing.T) {
	for _, c := range []Client{{}, {BaseURL: "http://example.com", Key: "x"}, {BaseURL: "https://example.com"}} {
		if c.Post(context.Background(), "/", nil, new(any)) == nil {
			t.Fatal("accepted config")
		}
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte("private-provider-payload"))
	}))
	defer s.Close()
	e := (Client{BaseURL: s.URL, Key: "x"}).Post(context.Background(), "/", nil, new(any))
	if e == nil || strings.Contains(e.Error(), "private") {
		t.Fatal(e)
	}
}
