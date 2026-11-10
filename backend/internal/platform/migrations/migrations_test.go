package migrations

import (
	"testing"
	"testing/fstest"
)

func TestRead(t *testing.T) {
	items, err := read(Files)
	if err != nil || len(items) != 4 {
		t.Fatal(err)
	}
	for _, source := range []fstest.MapFS{
		{}, {"sql/readme.txt": {Data: []byte("bad")}},
		{"sql/000001_a.sql": {}, "sql/000001_b.sql": {}},
		{"sql/subdir/file.sql": {}},
	} {
		if _, err := read(source); err == nil {
			t.Fatal("accepted invalid migration source")
		}
	}
}

func TestCompare(t *testing.T) {
	items := []migration{{name: "000001_a.sql", checksum: "a"}, {name: "000002_b.sql", checksum: "b"}}
	for _, tt := range []struct {
		applied      map[string]string
		complete, ok bool
	}{
		{map[string]string{}, false, true}, {map[string]string{"000001_a.sql": "a"}, false, true},
		{map[string]string{"000001_a.sql": "a", "000002_b.sql": "b"}, true, true},
		{map[string]string{}, true, false}, {map[string]string{"000001_a.sql": "changed"}, false, false},
		{map[string]string{"000002_b.sql": "b"}, false, false}, {map[string]string{"000003_unknown.sql": "x"}, false, false},
	} {
		if err := compare(items, tt.applied, tt.complete); (err == nil) != tt.ok {
			t.Fatalf("%+v: %v", tt, err)
		}
	}
}
