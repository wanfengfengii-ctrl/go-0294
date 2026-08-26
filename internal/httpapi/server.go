// Package httpapi exposes the assembly gate as a JSON HTTP API and serves the
// static single-page frontend. Every rejection is rendered in the unified
// {code,message,reasons,operation_id} shape, and reasons are deterministically
// sorted by the composite key (facade zone, plate, raw glass, furnace run,
// rack position, inspection grid, generation).
package httpapi

import (
	"encoding/json"
	"net/http"

	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/store"
)

// Server wires the store into an HTTP handler.
type Server struct {
	store    store.Store
	frontend string
	mux      *http.ServeMux
}

// New returns a Server backed by s, serving the frontend build directory.
func New(s store.Store, frontendDir string) *Server {
	srv := &Server{store: s, frontend: frontendDir, mux: http.NewServeMux()}
	srv.routes()
	return srv
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("POST /api/designs/lock", s.handleLockDesign)
	s.mux.HandleFunc("GET /api/tasks", s.handleListTasks)
	s.mux.HandleFunc("GET /api/tasks/{id}", s.handleGetTask)
	s.mux.HandleFunc("GET /api/tasks/{id}/lineage", s.handleGetLineage)
	s.mux.HandleFunc("GET /api/tasks/{id}/coverage", s.handleGetCoverage)
	s.mux.HandleFunc("POST /api/tasks/{id}/operations", s.handleAdvance)
	s.mux.HandleFunc("POST /api/tasks/{id}/samples", s.handleSamples)
	s.mux.HandleFunc("POST /api/tasks/{id}/instrument-calls", s.handleInstrument)
	s.mux.HandleFunc("POST /api/tasks/{id}/anomalies", s.handleAnomaly)
	s.mux.HandleFunc("GET /api/tasks/{id}/retests", s.handleGetRetests)
	s.mux.HandleFunc("POST /api/tasks/{id}/reviews", s.handleReview)
	s.mux.HandleFunc("POST /api/tasks/{id}/verdicts", s.handleVerdict)
	s.mux.HandleFunc("POST /api/retries/{id}/run", s.handleRunRetry)
	s.mux.HandleFunc("GET /api/retries", s.handleListRetries)
	s.mux.HandleFunc("GET /api/credentials/{id}", s.handleGetCredential)
	s.mux.HandleFunc("GET /api/film-ledger", s.handleFilmLedger)
	s.mux.Handle("/", http.FileServer(http.Dir(s.frontend)))
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	de, ok := err.(*domain.Error)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, domain.NewError("INTERNAL", err.Error()))
		return
	}
	writeJSON(w, statusFor(de.Code), de)
}

func statusFor(code domain.ErrorCode) int {
	switch code {
	case "NOT_FOUND":
		return http.StatusNotFound
	case "IDENTITY_DUPLICATE", "IDEMPOTENCY_CONFLICT", "LEASE_CONFLICT", "FINAL_EXISTS":
		return http.StatusConflict
	case "BAD_REQUEST":
		return http.StatusBadRequest
	default:
		return http.StatusUnprocessableEntity
	}
}
