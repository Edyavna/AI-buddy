package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/agent"
)

type example struct {
	ID        string
	Category  string
	Direction string
	Split     bool
	Text      string
}

type metrics struct {
	Correct int     `json:"correct"`
	Total   int     `json:"total"`
	Recall  float64 `json:"recall"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: hr-routing-eval PATH_TO_DATASET")
		os.Exit(2)
	}
	if err := run(os.Args[1], agent.FromEnv(), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string, model *agent.DeepSeek, output io.Writer) error {
	if model.Model == "" || model.Client.Key == "" || model.Client.BaseURL == "" {
		return fmt.Errorf("set LLM_MODEL, LLM_BASE_URL and LLM_API_KEY")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var data []example
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	if len(data) != 160 {
		return fmt.Errorf("expected 160 examples")
	}
	matrix := map[string]map[string]int{}
	byCategory := map[string]*metrics{}
	wrong := []map[string]any{}
	clarified, unsafe := 0, 0
	for _, item := range data {
		route, err := model.Classify(context.Background(), item.Text)
		direction := route.Direction
		if err != nil {
			direction = "error"
		}
		if direction == "needs_clarification" {
			clarified++
		}
		if item.Category == "hr" && direction == "mentor" {
			unsafe++
		}
		if matrix[item.Category] == nil {
			matrix[item.Category] = map[string]int{}
		}
		matrix[item.Category][direction]++
		if byCategory[item.Category] == nil {
			byCategory[item.Category] = &metrics{}
		}
		stats := byCategory[item.Category]
		stats.Total++
		if direction == item.Direction && route.Split == item.Split {
			stats.Correct++
		} else {
			wrong = append(wrong, map[string]any{"id": item.ID, "expected": item.Direction, "actual": direction, "expected_split": item.Split, "actual_split": route.Split})
		}
	}
	passed := unsafe == 0 && len(byCategory) == 4
	for _, stats := range byCategory {
		stats.Recall = float64(stats.Correct) / float64(stats.Total)
		passed = passed && stats.Total == 40 && stats.Correct >= 36
	}
	report := map[string]any{"model": model.Model, "utc": time.Now().UTC(), "temperature": 0, "timeout_seconds": 8,
		"confusion_matrix": matrix, "by_category": byCategory, "clarification_rate": float64(clarified) / float64(len(data)),
		"hr_to_mentor": unsafe, "errors": wrong, "passed": passed}
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return err
	}
	if !passed {
		return fmt.Errorf("classifier failed the agreed acceptance threshold")
	}
	return nil
}
