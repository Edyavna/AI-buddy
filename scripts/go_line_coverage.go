// Reports physical Go lines that contain source tokens inside instrumented blocks.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const module = "github.com/Edyavna/AI-buddy/backend/"

type block struct {
	startLine, startColumn, endLine, endColumn, count int
}

func readProfile(r io.Reader) (map[string][]block, error) {
	input := bufio.NewScanner(r)
	if !input.Scan() || !strings.HasPrefix(input.Text(), "mode: ") {
		return nil, errors.New("invalid Go coverage header")
	}
	files := map[string][]block{}
	for input.Scan() {
		file, fields, ok := strings.Cut(input.Text(), ":")
		var b block
		var statements int
		n, err := fmt.Sscanf(fields, "%d.%d,%d.%d %d %d",
			&b.startLine, &b.startColumn, &b.endLine, &b.endColumn, &statements, &b.count)
		if !ok || n != 6 || err != nil {
			return nil, fmt.Errorf("invalid coverage row: %q", input.Text())
		}
		if !strings.HasPrefix(file, module) {
			continue
		}
		file = strings.TrimPrefix(file, module)
		if !filepath.IsLocal(filepath.FromSlash(file)) {
			return nil, fmt.Errorf("invalid local path: %q", file)
		}
		if strings.HasPrefix(file, "internal/testkit/") || strings.HasSuffix(file, "_test.go") || statements == 0 {
			continue
		}
		files[file] = append(files[file], b)
	}
	return files, input.Err()
}

func (b block) contains(p token.Position) bool {
	if p.Line < b.startLine || p.Line > b.endLine {
		return false
	}
	if p.Line == b.startLine && p.Column < b.startColumn {
		return false
	}
	return p.Line != b.endLine || p.Column < b.endColumn
}

func countLines(source []byte, blocks []block) (covered, total int, err error) {
	f := token.NewFileSet().AddFile("", -1, len(source))
	var tokens scanner.Scanner
	tokens.Init(f, source, func(p token.Position, message string) {
		if err == nil {
			err = fmt.Errorf("%s: %s", p, message)
		}
	}, 0)
	lines := map[int]bool{}
	for {
		pos, kind, _ := tokens.Scan()
		switch kind {
		case token.EOF:
			for _, hit := range lines {
				if hit {
					covered++
				}
			}
			return covered, len(lines), err
		case token.SEMICOLON, token.LBRACE, token.RBRACE, token.LPAREN, token.RPAREN,
			token.LBRACK, token.RBRACK, token.COMMA, token.COLON:
			continue
		}
		p := f.PositionFor(pos, false)
		for _, b := range blocks {
			if b.contains(p) {
				lines[p.Line] = lines[p.Line] || b.count > 0
			}
		}
	}
}

func report(profile string) (covered, total int, err error) {
	f, err := os.Open(profile)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	files, err := readProfile(f)
	if err != nil {
		return 0, 0, err
	}
	for path, blocks := range files {
		source, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			return 0, 0, err
		}
		hits, lines, err := countLines(source, blocks)
		if err != nil {
			return 0, 0, fmt.Errorf("%s: %w", path, err)
		}
		covered += hits
		total += lines
	}
	if total == 0 {
		return 0, 0, errors.New("no instrumented application lines")
	}
	return covered, total, nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage (from backend): go run ../scripts/go_line_coverage.go coverage.out")
		os.Exit(2)
	}
	covered, total, err := report(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Go line coverage: %.2f%% (%d/%d instrumented source lines)\n",
		100*float64(covered)/float64(total), covered, total)
}
