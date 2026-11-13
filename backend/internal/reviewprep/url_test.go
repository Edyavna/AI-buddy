package reviewprep

import (
	"testing"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func TestParsePRURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		repo    string
		number  int
		invalid bool
	}{
		{name: "https", url: "https://github.com/Edyavna/AI-buddy/pull/59", repo: "Edyavna/AI-buddy", number: 59},
		{name: "http", url: "http://github.com/owner/repo/pull/1", repo: "owner/repo", number: 1},
		{name: "trailing slash", url: "https://github.com/owner/repo/pull/42/", repo: "owner/repo", number: 42},
		{name: "dots and dashes", url: "https://github.com/o.w-ner/r_e-po/pull/7", repo: "o.w-ner/r_e-po", number: 7},
		{name: "empty", url: "  ", invalid: true},
		{name: "not github", url: "https://gitlab.com/owner/repo/pull/1", invalid: true},
		{name: "issues instead of pull", url: "https://github.com/owner/repo/issues/1", invalid: true},
		{name: "file path", url: "https://github.com/owner/repo/pull/1/files", invalid: true},
		{name: "missing number", url: "https://github.com/owner/repo/pull/", invalid: true},
		{name: "zero", url: "https://github.com/owner/repo/pull/0", invalid: true},
		{name: "huge number", url: "https://github.com/owner/repo/pull/99999999999", invalid: true},
		{name: "double slash", url: "https://github.com/owner//pull/1", invalid: true},
		{name: "no scheme", url: "github.com/owner/repo/pull/1", invalid: true},
		{name: "ftp", url: "ftp://github.com/owner/repo/pull/1", invalid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, number, err := ParsePRURL(tc.url)
			if tc.invalid {
				if err == nil {
					t.Fatalf("expected error for %q, got %q #%d", tc.url, repo, number)
				}
				if err != ports.ErrInvalid {
					t.Fatalf("expected ErrInvalid, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if repo != tc.repo || number != tc.number {
				t.Fatalf("got %q #%d, want %q #%d", repo, number, tc.repo, tc.number)
			}
		})
	}
}
