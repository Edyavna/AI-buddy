// knowledge-import is an operator command, never exposed to the browser.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"os"
	"time"
)

func main() {
	dir := flag.String("dir", "../demo/knowledge", "Markdown manifest directory")
	schema := flag.Bool("schema", false, "Print proposed schema for migration owner")
	flag.Parse()
	if *schema {
		fmt.Println(knowledge.Schema)
		return
	}
	if run(*dir) != nil {
		fmt.Fprintln(os.Stderr, "Knowledge import failed; check database, manifest and embedding configuration")
		os.Exit(1)
	}
}
func run(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
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
