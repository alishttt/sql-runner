package runner

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Result struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	RowCount  int      `json:"row_count"`
	ElapsedMs int64    `json:"elapsed_ms"`
}

type ExecResult struct {
	RowsAffected int64 `json:"rows_affected"`
	LastInsertID int64 `json:"last_insert_id,omitempty"`
	ElapsedMs    int64 `json:"elapsed_ms"`
}

type Runner struct {
	db *sql.DB
}

type Option func(*config)

type config struct {
	maxOpen  int
	maxIdle  int
	connLife time.Duration
	connIdle time.Duration
}

func defaultConfig() config {
	return config{
		maxOpen:  10,
		maxIdle:  5,
		connLife: 30 * time.Minute,
		connIdle: 5 * time.Minute,
	}
}

func WithMaxOpenConns(n int) Option { return func(c *config) { c.maxOpen = n } }

func WithMaxIdleConns(n int) Option { return func(c *config) { c.maxIdle = n } }

func WithConnMaxLifetime(d time.Duration) Option { return func(c *config) { c.connLife = d } }

func WithConnMaxIdleTime(d time.Duration) Option { return func(c *config) { c.connIdle = d } }

func New(driver, dsn string, opts ...Option) (*Runner, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	db.SetMaxOpenConns(cfg.maxOpen)
	db.SetMaxIdleConns(cfg.maxIdle)
	db.SetConnMaxLifetime(cfg.connLife)
	db.SetConnMaxIdleTime(cfg.connIdle)

	return &Runner{db: db}, nil
}

func (r *Runner) Close() error { return r.db.Close() }

func (r *Runner) Stats() sql.DBStats { return r.db.Stats() }

func (r *Runner) Query(ctx context.Context, sqlText string, args ...any) (*Result, error) {
	start := time.Now()

	rows, err := r.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("columns: %w", err)
	}

	out := &Result{Columns: cols, Rows: [][]any{}}

	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				values[i] = string(b)
			}
		}
		out.Rows = append(out.Rows, values)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	out.RowCount = len(out.Rows)
	out.ElapsedMs = time.Since(start).Milliseconds()
	return out, nil
}

func (r *Runner) Exec(ctx context.Context, sqlText string, args ...any) (*ExecResult, error) {
	start := time.Now()

	res, err := r.db.ExecContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("exec: %w", err)
	}

	out := &ExecResult{ElapsedMs: time.Since(start).Milliseconds()}
	if affected, err := res.RowsAffected(); err == nil {
		out.RowsAffected = affected
	}
	if id, err := res.LastInsertId(); err == nil {
		out.LastInsertID = id
	}
	return out, nil
}
