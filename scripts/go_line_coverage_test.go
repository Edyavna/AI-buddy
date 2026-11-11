package main

import (
	"strings"
	"testing"
)

func TestPhysicalLineCoverage(t *testing.T) {
	source := []byte(`package sample
func f() {
	run()
	// Comment-only line.
	if flag {
		skip()
	}
	multi(
		value,
	)
}
`)
	profile := "mode: atomic\n" +
		module + "sample.go:2.10,5.10 2 1\n" +
		module + "sample.go:5.10,7.3 1 0\n" +
		module + "sample.go:8.2,10.3 1 1\n" +
		module + "sample.go:8.2,8.8 1 0\n" +
		module + "internal/testkit/ignored.go:1.1,2.2 1 1\n"
	files, err := readProfile(strings.NewReader(profile))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	hits, lines, err := countLines(source, files["sample.go"])
	if hits != 4 || lines != 5 || err != nil {
		t.Fatalf("got %d/%d: %v", hits, lines, err)
	}
}

func TestMalformedProfile(t *testing.T) {
	for _, profile := range []string{"", "mode: atomic\ninvalid", "mode: atomic\n" + module + "../escape.go:1.1,1.2 1 1"} {
		if _, err := readProfile(strings.NewReader(profile)); err == nil {
			t.Errorf("accepted malformed profile: %q", profile)
		}
	}
}
