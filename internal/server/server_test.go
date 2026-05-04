package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alishttt/sql-runner/internal/runner"
	"github.com/alishttt/sql-runner/internal/server"

	_ "modernc.org/sqlite"
)

func setup(t *testing.T) *runner.Runner {
	t.Helper()
	r, err := runner.New("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	ctx := context.Background()
	for _, s := range []string{
		`CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)`,
		`INSERT INTO t (id, name) VALUES (1, 'alice')`,
		`INSERT INTO t (id, name) VALUES (2, 'bob')`,
	} {
		if _, err := r.Exec(ctx, s); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return r
}

func doRequest(t *testing.T, srv *http.Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Buffer
	if body != "" {
		reqBody = bytes.NewBufferString(body)
	} else {
		reqBody = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, reqBody)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	return rec
}

func TestHealth_OK(t *testing.T) {
	r := setup(t)
	srv := server.New(":0", r)

	rec := doRequest(t, srv, http.MethodGet, "/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
}

func TestQuery_Success(t *testing.T) {
	r := setup(t)
	srv := server.New(":0", r)

	rec := doRequest(t, srv, http.MethodPost, "/query",
		`{"sql":"SELECT id, name FROM t ORDER BY id"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp runner.Result
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RowCount != 2 {
		t.Fatalf("rowCount: got %d, want 2", resp.RowCount)
	}
}

func TestQuery_WithArgs(t *testing.T) {
	r := setup(t)
	srv := server.New(":0", r)

	rec := doRequest(t, srv, http.MethodPost, "/query",
		`{"sql":"SELECT name FROM t WHERE id = ?","args":[2]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp runner.Result
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.RowCount != 1 || resp.Rows[0][0] != "bob" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestQuery_BadJSON_Returns400(t *testing.T) {
	r := setup(t)
	srv := server.New(":0", r)

	rec := doRequest(t, srv, http.MethodPost, "/query", `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestQuery_MissingSQL_Returns400(t *testing.T) {
	r := setup(t)
	srv := server.New(":0", r)

	rec := doRequest(t, srv, http.MethodPost, "/query", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestQuery_GET_Returns405(t *testing.T) {
	r := setup(t)
	srv := server.New(":0", r)

	rec := doRequest(t, srv, http.MethodGet, "/query", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want 405", rec.Code)
	}
}

func TestExec_InsertAndQuery(t *testing.T) {
	r := setup(t)
	srv := server.New(":0", r)

	rec := doRequest(t, srv, http.MethodPost, "/exec",
		`{"sql":"INSERT INTO t (id, name) VALUES (?, ?)","args":[3,"carol"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("exec status: got %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodPost, "/query",
		`{"sql":"SELECT count(*) FROM t"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("query status: got %d, want 200", rec.Code)
	}
}

func TestPoolStats(t *testing.T) {
	r := setup(t)
	srv := server.New(":0", r)

	rec := doRequest(t, srv, http.MethodGet, "/pool/stats", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
}
