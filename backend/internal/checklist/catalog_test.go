package checklist

import (
	"encoding/csv"
	"os"
	"strings"
	"testing"
)

func TestCatalogMatchesSource(t *testing.T) {
	f, err := os.Open("source.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	source, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := load(catalogJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != len(entries)+1 {
		t.Fatalf("source has %d rows, catalog has %d", len(source)-1, len(entries))
	}
	for i, entry := range entries {
		row := source[i+1]
		if entry.Title != row[0] || entry.Description != row[1] || entry.Period != row[2] {
			t.Fatalf("source row %d differs from the catalog", i+1)
		}
		if row[1] != "" && entry.instructions() != row[1] {
			t.Fatalf("instruction for row %d lost content", i+1)
		}
	}
	for _, pair := range [][2]int{{10, 12}, {14, 15}} {
		left, right := entries[pair[0]], entries[pair[1]]
		if left.Title != right.Title || left.Key == right.Key {
			t.Fatalf("duplicate titles must retain distinct keys: %s, %s", left.Key, right.Key)
		}
	}
	if entries[14].Policy != "intern" || entries[15].Policy != "mentor" ||
		entries[14].PolicyNote == "" || entries[15].PolicyNote == "" {
		t.Fatal("survey role assumptions must be explicit")
	}
}

func TestCatalogRejectsInvalidData(t *testing.T) {
	for _, tc := range []struct{ old, replacement string }{
		{`"mentor-guide"`, `"team-setup"`},
		{`"completion_policy": "both"`, `"completion_policy": "unknown"`},
		{`"source_row": 2`, `"source_row": 1`},
		{`"url": null`, `"url": "javascript:alert(1)"`},
	} {
		data := strings.Replace(string(catalogJSON), tc.old, tc.replacement, 1)
		if _, err := load([]byte(data)); err == nil {
			t.Errorf("invalid catalog accepted: %s", tc.replacement)
		}
	}
}
