package knowledge

import (
	"math"
	"testing"
)

func TestVectors(t *testing.T) {
	v := make([]float64, 1024)
	if _, e := vector(v); e == nil {
		t.Fatal("zero")
	}
	v[0] = 1
	if _, e := vector(v); e != nil {
		t.Fatal(e)
	}
	v[0] = math.NaN()
	if _, e := vector(v); e == nil {
		t.Fatal("NaN")
	}
	if _, e := vector(nil); e == nil {
		t.Fatal("dimension")
	}
}
func TestDemoLoader(t *testing.T) {
	docs, e := LoadMarkdown("../../../demo/knowledge")
	if e != nil || len(docs) != 10 {
		t.Fatal(len(docs), e)
	}
	seen := map[string]bool{}
	for _, d := range docs {
		if seen[d.ID] || d.Content == "" || d.UpdatedAt.IsZero() {
			t.Fatal(d.ID)
		}
		seen[d.ID] = true
	}
	if !seen["doc_local_setup"] {
		t.Fatal("stable ID missing")
	}
}
