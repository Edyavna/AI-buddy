// knowledge-import is an operator command, never exposed to the browser.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
)

func main() {
	dir := flag.String("dir", "../demo/knowledge", "Markdown manifest directory")
	wikipediaDir := flag.String("wikipedia-dir", "", "Import the pinned Wikipedia snapshot from this directory")
	schema := flag.Bool("schema", false, "Print the knowledge migration")
	flag.Parse()
	if *schema {
		sql, err := migrations.Files.ReadFile("sql/000007_knowledge.sql")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Knowledge migration not available")
			os.Exit(1)
		}
		fmt.Print(string(sql))
		return
	}
	if err := run(*dir, *wikipediaDir); err != nil {
		fmt.Fprintln(os.Stderr, "Knowledge import failed; check database, manifest and embedding configuration")
		if *wikipediaDir != "" {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
func run(dir, wikipediaDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if wikipediaDir != "" {
		return importWikipedia(ctx, wikipediaDir)
	}
	docs, e := knowledge.LoadMarkdown(dir)
	if e != nil {
		return e
	}
	db, e := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if e != nil {
		return e
	}
	defer db.Close()
	s := knowledge.Service{DB: db, Embedder: knowledge.JinaFromEnv()}
	for _, d := range docs {
		if e = s.Import(ctx, d); e != nil {
			return e
		}
	}
	fmt.Printf("Imported %d synthetic documents\n", len(docs))
	return nil
}

func importWikipedia(ctx context.Context, dir string) error {
	db, err := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("could not connect to the knowledge database")
	}
	defer db.Close()
	s := knowledge.Service{DB: db, Embedder: knowledge.JinaFromEnv()}
	if err := s.ImportWikipedia(ctx, dir); err != nil {
		return err
	}
	fmt.Println("Wikipedia snapshot ready")
	return nil
}
