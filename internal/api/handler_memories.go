package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/russellhaering/wasmdb/internal/memory"
)

func (s *Server) handleCreateMemory(w http.ResponseWriter, r *http.Request) {
	if s.memoryStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "memory store not available"})
		return
	}
	session := SessionFromContext(r.Context())
	if session == nil {
		writeError(w, ErrUnauthorized)
		return
	}

	var req struct {
		Scope   string   `json:"scope"`
		Title   string   `json:"title"`
		Summary string   `json:"summary"`
		Tags    []string `json:"tags"`
		Pinned  bool     `json:"pinned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}

	m, err := s.memoryStore.Create(r.Context(), &memory.Memory{
		UserID:  session.UserID,
		Scope:   req.Scope,
		Title:   req.Title,
		Summary: req.Summary,
		Tags:    req.Tags,
		Pinned:  req.Pinned,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
	if s.memoryStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "memory store not available"})
		return
	}
	session := SessionFromContext(r.Context())
	if session == nil {
		writeError(w, ErrUnauthorized)
		return
	}

	entries, err := s.memoryStore.ListCatalog(r.Context(), session.UserID, 200)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	if s.memoryStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "memory store not available"})
		return
	}
	session := SessionFromContext(r.Context())
	if session == nil {
		writeError(w, ErrUnauthorized)
		return
	}

	id := r.PathValue("id")
	m, err := s.memoryStore.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if m == nil || m.UserID != session.UserID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "memory not found"})
		return
	}
	_ = s.memoryStore.Touch(r.Context(), m.ID)
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleUpdateMemory(w http.ResponseWriter, r *http.Request) {
	if s.memoryStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "memory store not available"})
		return
	}
	session := SessionFromContext(r.Context())
	if session == nil {
		writeError(w, ErrUnauthorized)
		return
	}

	id := r.PathValue("id")
	existing, err := s.memoryStore.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if existing == nil || existing.UserID != session.UserID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "memory not found"})
		return
	}

	var req struct {
		Scope      string   `json:"scope"`
		Title      string   `json:"title"`
		Summary    string   `json:"summary"`
		Tags       []string `json:"tags"`
		Pinned     bool     `json:"pinned"`
		LastUsedAt string   `json:"last_used_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, ErrBadRequest)
		return
	}

	patch := &memory.Memory{
		Scope:   req.Scope,
		Title:   req.Title,
		Summary: req.Summary,
		Tags:    req.Tags,
		Pinned:  req.Pinned,
	}
	if req.LastUsedAt != "" {
		if t, err := time.Parse(time.RFC3339, req.LastUsedAt); err == nil {
			patch.LastUsedAt = t
		}
	}

	updated, err := s.memoryStore.Update(r.Context(), id, patch)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	if s.memoryStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "memory store not available"})
		return
	}
	session := SessionFromContext(r.Context())
	if session == nil {
		writeError(w, ErrUnauthorized)
		return
	}

	id := r.PathValue("id")
	existing, err := s.memoryStore.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if existing == nil || existing.UserID != session.UserID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "memory not found"})
		return
	}

	if err := s.memoryStore.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMemoryCatalog(w http.ResponseWriter, r *http.Request) {
	if s.memoryStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "memory store not available"})
		return
	}
	session := SessionFromContext(r.Context())
	if session == nil {
		writeError(w, ErrUnauthorized)
		return
	}

	limit := 25
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	entries, err := s.memoryStore.ListCatalog(r.Context(), session.UserID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memories": entries})
}
