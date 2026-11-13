package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/hrscope"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	list := len(args) == 1 && args[0] == "list"
	update := len(args) == 3 && (args[0] == "grant" || args[0] == "revoke")
	if !list && !update {
		return errors.New("usage: hr-scope list | hr-scope grant/revoke HR_USER_ID TEAM_ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("could not connect to PostgreSQL")
	}
	defer db.Close()
	if err := migrations.Verify(ctx, db, migrations.Files); err != nil {
		return errors.New("apply migrations first")
	}
	store := hrscope.Store{DB: db}
	if list {
		rows, err := store.List(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(rows)
	}
	return store.Set(ctx, args[1], args[2], args[0] == "grant")
}
