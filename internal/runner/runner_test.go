package runner_test

import (
	"context"
	"testing"
	"time"

	"github.com/alishttt/sql-runner/internal/runner"

	_ "modernc.org/sqlite"
)

func newTestRunner(t *testing.T) *runner.Runner {
	t.Helper()
	r, err := runner.New("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	ctx := context.Background()
	stmts := []string{
		`CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)`,
		`INSERT INTO t (id, name) VALUES (1, 'alice')`,
		`INSERT INTO t (id, name) VALUES (2, 'bob')`,
		`INSERT INTO t (id, name) VALUES (3, 'carol')`,
	}
	for _, s := range stmts {
		if _, err := r.Exec(ctx, s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
	return r
}

func TestQuery_ReturnsRows(t *testing.T) {
	r := newTestRunner(t)

	res, err := r.Query(context.Background(), `SELECT id, name FROM t ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got, want := res.RowCount, 3; got != want {
		t.Fatalf("row count: got %d, want %d", got, want)
	}
	if len(res.Columns) != 2 || res.Columns[0] != "id" || res.Columns[1] != "name" {
		t.Fatalf("columns: got %v, want [id name]", res.Columns)
	}
	if name := res.Rows[0][1]; name != "alice" {
		t.Fatalf("first row name: got %v, want alice", name)
	}
}

func TestQuery_WithArgs(t *testing.T) {
	r := newTestRunner(t)

	res, err := r.Query(context.Background(), `SELECT name FROM t WHERE id = ?`, 2)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if res.RowCount != 1 {
		t.Fatalf("row count: got %d, want 1", res.RowCount)
	}
	if name := res.Rows[0][0]; name != "bob" {
		t.Fatalf("name: got %v, want bob", name)
	}
}

func TestQuery_RespectsCancelledContext(t *testing.T) {
	r := newTestRunner(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	if _, err := r.Query(ctx, `SELECT 1`); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestQuery_RespectsTimeout(t *testing.T) {
	r := newTestRunner(t)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond)

	if _, err := r.Query(ctx, `SELECT 1`); err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestExec_ReportsRowsAffected(t *testing.T) {
	r := newTestRunner(t)

	res, err := r.Exec(context.Background(), `UPDATE t SET name = 'updated' WHERE id <= ?`, 2)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.RowsAffected != 2 {
		t.Fatalf("rows affected: got %d, want 2", res.RowsAffected)
	}
}

func TestStats_ReportsPoolUsage(t *testing.T) {
	r := newTestRunner(t)

	if _, err := r.Query(context.Background(), `SELECT 1`); err != nil {
		t.Fatalf("query: %v", err)
	}

	stats := r.Stats()
	if stats.OpenConnections < 1 {
		t.Fatalf("expected at least 1 open connection, got %d", stats.OpenConnections)
	}
}
