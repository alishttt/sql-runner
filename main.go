package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alishttt/sql-runner/internal/runner"
	"github.com/alishttt/sql-runner/internal/server"

	_ "modernc.org/sqlite"
)

const (
	httpAddr        = ":8085"
	shutdownTimeout = 10 * time.Second
)

func main() {
	runCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	r, err := runner.New("sqlite", "file::memory:?cache=shared",
		runner.WithMaxOpenConns(10),
		runner.WithMaxIdleConns(5),
		runner.WithConnMaxLifetime(30*time.Minute),
		runner.WithConnMaxIdleTime(5*time.Minute),
	)
	if err != nil {
		log.Fatalf("runner: init failed: %v", err)
	}
	defer r.Close()

	if err := seed(runCtx, r); err != nil {
		log.Fatalf("runner: seed failed: %v", err)
	}
	log.Println("seeded in-memory database with sample data")

	srv := server.New(httpAddr, r)

	go func() {
		log.Printf("listening on %s", httpAddr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http: server error: %v", err)
			stop()
		}
	}()

	<-runCtx.Done()
	log.Println("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http: graceful shutdown error: %v", err)
		os.Exit(1)
	}
	log.Println("clean exit")
}

func seed(ctx context.Context, r *runner.Runner) error {
	stmts := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`INSERT INTO users (name, email) VALUES ('Alice',   'alice@example.com')`,
		`INSERT INTO users (name, email) VALUES ('Bob',     'bob@example.com')`,
		`INSERT INTO users (name, email) VALUES ('Charlie', 'charlie@example.com')`,
		`CREATE TABLE orders (
			id INTEGER PRIMARY KEY,
			user_id INTEGER NOT NULL,
			amount REAL NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`INSERT INTO orders (user_id, amount) VALUES (1,  99.99)`,
		`INSERT INTO orders (user_id, amount) VALUES (1, 150.00)`,
		`INSERT INTO orders (user_id, amount) VALUES (2,  50.25)`,
		`INSERT INTO orders (user_id, amount) VALUES (3,  75.00)`,
	}
	for _, s := range stmts {
		if _, err := r.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}
