package httpapi

import (
	"encoding/json"
	"net/http"

	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/store"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleLockDesign(w http.ResponseWriter, r *http.Request) {
	var snap domain.DesignSnapshot
	if err := json.NewDecoder(r.Body).Decode(&snap); err != nil {
		writeErr(w, domain.NewError("BAD_REQUEST", "invalid JSON body"))
		return
	}
	task, err := s.store.LockDesign(snap)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListTasks())
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	task, err := s.store.GetTask(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleGetLineage(w http.ResponseWriter, r *http.Request) {
	lineage, err := s.store.GetLineage(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lineage)
}

func (s *Server) handleGetCoverage(w http.ResponseWriter, r *http.Request) {
	matrix, err := s.store.GetCoverage(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, matrix)
}

func (s *Server) handleAdvance(w http.ResponseWriter, r *http.Request) {
	var req store.OperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, domain.NewError("BAD_REQUEST", "invalid JSON body"))
		return
	}
	task, err := s.store.Advance(r.PathValue("id"), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleSamples(w http.ResponseWriter, r *http.Request) {
	var req store.SampleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, domain.NewError("BAD_REQUEST", "invalid JSON body"))
		return
	}
	result, err := s.store.SubmitSamples(r.PathValue("id"), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleInstrument(w http.ResponseWriter, r *http.Request) {
	var req store.InstrumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, domain.NewError("BAD_REQUEST", "invalid JSON body"))
		return
	}
	result, err := s.store.SubmitInstrumentCall(r.PathValue("id"), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRunRetry(w http.ResponseWriter, r *http.Request) {
	result, err := s.store.RunRetry(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleListRetries(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.PendingRetries())
}

func (s *Server) handleAnomaly(w http.ResponseWriter, r *http.Request) {
	var req store.AnomalyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, domain.NewError("BAD_REQUEST", "invalid JSON body"))
		return
	}
	set, err := s.store.RegisterAnomaly(r.PathValue("id"), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

func (s *Server) handleGetRetests(w http.ResponseWriter, r *http.Request) {
	set, err := s.store.GetRetests(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if set == nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, set)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	var req store.ReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, domain.NewError("BAD_REQUEST", "invalid JSON body"))
		return
	}
	if err := s.store.AddReview(r.PathValue("id"), req); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (s *Server) handleVerdict(w http.ResponseWriter, r *http.Request) {
	var req store.VerdictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, domain.NewError("BAD_REQUEST", "invalid JSON body"))
		return
	}
	result, err := s.store.SubmitVerdict(r.PathValue("id"), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetCredential(w http.ResponseWriter, r *http.Request) {
	cred, err := s.store.GetCredential(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cred)
}

func (s *Server) handleFilmLedger(w http.ResponseWriter, r *http.Request) {
	ledger, err := s.store.GetFilmLedger(r.URL.Query().Get("batch"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ledger)
}
