package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Edyavna/AI-buddy/backend/internal/integrations"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/database"
	"github.com/Edyavna/AI-buddy/backend/internal/platform/migrations"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return errors.New("Проверьте конфигурацию окружения")
	}
	if !cfg.DemoMode || !cfg.DatabaseEnabled {
		return errors.New("Требуются DEMO_MODE=true и DATABASE_ENABLED=true")
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.StartupTimeout)
	defer cancel()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("Не удалось подключиться к БД")
	}
	defer db.Close()
	if err := migrations.Verify(ctx, db, migrations.Files); err != nil {
		return errors.New("Сначала выполните миграции")
	}
	control, err := integrations.NewControl(db, cfg.DemoMode)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Синтетические данные AI-buddy")
	err = execute(ctx, control, args, out)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return errors.New("Заявка не найдена")
	case errors.Is(err, ports.ErrConflict):
		return errors.New("Решение уже принято, противоположное решение запрещено")
	case errors.Is(err, ports.ErrInvalid):
		return errors.New("Некорректные параметры команды")
	case errors.Is(err, ports.ErrUnavailable):
		return errors.New("Хранилище имитации временно недоступно")
	default:
		return err
	}
}

func execute(ctx context.Context, c *integrations.Control, args []string, out io.Writer) error {
	if len(args) == 0 {
		return usage()
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return usage()
		}
		rows, err := c.List(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(rows)
	case "count":
		if len(args) != 1 {
			return usage()
		}
		count, err := c.Count(ctx)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, count)
		return err
	case "approve":
		if len(args) != 2 {
			return usage()
		}
		return c.Decide(ctx, args[1], "granted", "")
	case "reject":
		if len(args) != 3 {
			return usage()
		}
		return c.Decide(ctx, args[1], "rejected", args[2])
	case "fault":
		if len(args) != 3 {
			return usage()
		}
		return c.SetFault(ctx, args[1], args[2])
	default:
		return usage()
	}
}

func usage() error {
	return errors.New("Команды: list | count | approve REQUEST_ID | reject REQUEST_ID ПРИЧИНА | fault REQUEST_ID none|reject-before-write|timeout-after-commit|lookup-unavailable")
}
