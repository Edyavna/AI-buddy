package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Edyavna/AI-buddy/backend/internal/platform/config"
)

func TestServeGracefulShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("done"))
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, server, listener, time.Second) }()
	response := make(chan error, 1)
	go func() {
		r, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			_, err = io.ReadAll(r.Body)
			_ = r.Body.Close()
		}
		response <- err
	}()
	select {
	case <-started:
	case err := <-response:
		t.Fatalf("request failed before reaching handler: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("request never started")
	}
	cancel()
	close(release)
	if err := <-response; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRunWithoutDatabase(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, config.Config{HTTPAddr: "127.0.0.1:0", ShutdownTimeout: time.Second}, logger); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "database disabled") {
		t.Fatal(logs.String())
	}
	if err := Run(ctx, config.Config{HTTPAddr: "bad"}, logger); err == nil {
		t.Fatal("bad listener accepted")
	}
	err := Run(ctx, config.Config{DatabaseEnabled: true, DatabaseURL: "postgres://user:secret@127.0.0.1:1/db", StartupTimeout: time.Second}, logger)
	if err == nil || strings.Contains(err.Error()+logs.String(), "secret") {
		t.Fatalf("%v %s", err, logs.String())
	}
}
