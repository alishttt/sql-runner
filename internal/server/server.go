package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/alishttt/sql-runner/internal/runner"
)

const (
	defaultQueryTimeout = 30 * time.Second
	maxQueryTimeout     = 5 * time.Minute
)

type queryRequest struct {
	SQL       string `json:"sql"`
	Args      []any  `json:"args,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func New(addr string, r *runner.Runner) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("POST /query", queryHandler(r))
	mux.HandleFunc("POST /exec", execHandler(r))
	mux.HandleFunc("GET /pool/stats", statsHandler(r))

	return &http.Server{
		Addr:              addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      maxQueryTimeout + 10*time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func queryHandler(r *runner.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body queryRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON: " + err.Error()})
			return
		}
		if body.SQL == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "field 'sql' is required"})
			return
		}

		queryCtx, cancel := context.WithTimeout(req.Context(), pickTimeout(body.TimeoutMs))
		defer cancel()

		result, err := r.Query(queryCtx, body.SQL, body.Args...)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				writeJSON(w, http.StatusGatewayTimeout, errorResponse{Error: "query timeout exceeded"})
				return
			}
			if errors.Is(err, context.Canceled) {
				writeJSON(w, 499, errorResponse{Error: "client disconnected"}) // 499 = nginx-style "client closed request"
				return
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func execHandler(r *runner.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body queryRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON: " + err.Error()})
			return
		}
		if body.SQL == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "field 'sql' is required"})
			return
		}

		queryCtx, cancel := context.WithTimeout(req.Context(), pickTimeout(body.TimeoutMs))
		defer cancel()

		result, err := r.Exec(queryCtx, body.SQL, body.Args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func statsHandler(r *runner.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, r.Stats())
	}
}

func pickTimeout(requestedMs int) time.Duration {
	if requestedMs <= 0 {
		return defaultQueryTimeout
	}
	d := time.Duration(requestedMs) * time.Millisecond
	if d > maxQueryTimeout {
		return maxQueryTimeout
	}
	return d
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		log.Printf("%s %s -> %d (%dms)", r.Method, r.URL.Path, ww.status, time.Since(start).Milliseconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
