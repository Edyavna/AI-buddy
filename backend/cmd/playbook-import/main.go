// playbook-import indexes a pinned AvitoTech Playbook snapshot.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/knowledge"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/identity"
)

func main() {
	repo := flag.String("repo", "", "Path to a local avito-tech/playbook Git clone")
	commit := flag.String("commit", "", "Full SHA of the upstream Playbook commit")
	check := flag.Bool("check", false, "Check the snapshot and count parts without writing to PostgreSQL or calling Jina")
	query := flag.String("query", "", "Show the five best matches for a question without importing a snapshot")
	direction := flag.String("direction", "mentor", "Search direction for -query: mentor or hr")
	intern := flag.String("intern", "intern_demo_1", "Intern ID for -query")
	team := flag.String("team", "team_demo_backend", "Team ID for -query")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var err error
	if *query != "" {
		ctx = identity.WithPrincipal(ctx, identity.Principal{UserID: *intern, TeamID: *team, Role: identity.Intern})
		err = search(ctx, *query, *direction, os.Stdout)
	} else {
		err = run(ctx, *repo, *commit, *check)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func search(ctx context.Context, query, direction string, output io.Writer) error {
	if strings.TrimSpace(query) == "" || (direction != "mentor" && direction != "hr") {
		return fmt.Errorf("provide a question and choose mentor or hr")
	}
	service, err := openKnowledge(ctx)
	if err != nil {
		return err
	}
	defer service.DB.Close()
	hits, err := service.SearchDirection(ctx, query, direction)
	if err != nil {
		return fmt.Errorf("search knowledge: %w", err)
	}
	for i, hit := range hits {
		doc, err := service.Read(ctx, hit.DocumentID)
		if err != nil {
			return fmt.Errorf("read search result: %w", err)
		}
		if _, err := fmt.Fprintf(output, "%d. %s [%s] %s\n", i+1, hit.Title, hit.DocumentID, doc.OriginalURL); err != nil {
			return err
		}
	}
	if len(hits) == 0 {
		_, err = fmt.Fprintln(output, "No matching documents")
	}
	return err
}

func run(ctx context.Context, repo, commit string, check bool) error {
	snapshot, err := knowledge.LoadPlaybook(ctx, repo, commit)
	if err != nil {
		return err
	}
	fmt.Printf("Playbook %s: %d files, %d parts\n", snapshot.Commit, snapshot.Files, snapshot.Parts())
	if check {
		return nil
	}
	service, err := openKnowledge(ctx)
	if err != nil {
		return err
	}
	defer service.DB.Close()
	result, err := service.SyncPlaybook(ctx, snapshot)
	if err != nil {
		return err
	}
	fmt.Printf("Playbook synchronized: %d indexed, %d reused, %d archived\n", result.Indexed, result.Reused, result.Archived)
	return nil
}

func openKnowledge(ctx context.Context) (*knowledge.Service, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("set DATABASE_URL to a migrated AI-buddy PostgreSQL database")
	}
	db, err := database.Open(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("could not connect to PostgreSQL; check DATABASE_URL")
	}
	return &knowledge.Service{DB: db, Embedder: knowledge.JinaFromEnv()}, nil
}
