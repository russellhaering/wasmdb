package api

import (
	"errors"
	"net/http"

	"github.com/russellhaering/moraine/index"
	"github.com/russellhaering/moraine/indexed"
)

// writeSearchError maps moraine/indexed search sentinels to HTTP responses.
// Index-readiness problems are transient (503); configuration problems where
// the requested search mode isn't available for the table are client errors
// (400); anything else is a 500.
func writeSearchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, indexed.ErrIndexNotReady):
		writeErrorMsg(w, http.StatusServiceUnavailable, "index_not_ready",
			"indexes are still being built; retry shortly")
	case errors.Is(err, indexed.ErrIndexDegraded):
		writeErrorMsg(w, http.StatusServiceUnavailable, "index_degraded", err.Error())
	case errors.Is(err, indexed.ErrFullTextUnavailable),
		errors.Is(err, indexed.ErrVectorUnavailable):
		writeErrorMsg(w, http.StatusBadRequest, "search_unavailable", err.Error())
	default:
		writeErrorMsg(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

type vectorSearchRequest struct {
	Vector []float32 `json:"vector,omitempty"`
	Query  string    `json:"query,omitempty"`
	K      int       `json:"k"`
}

type textSearchRequest struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type attributeSearchRequest struct {
	Filters []index.Filter `json:"filters"`
	Limit   int            `json:"limit"`
	Offset  int            `json:"offset"`
}

func (s *Server) handleVectorSearch(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	table, err := s.registry.GetTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}

	var req vectorSearchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	if req.K <= 0 {
		req.K = 10
	}

	var docs any
	if len(req.Vector) > 0 {
		docs, err = table.SearchVector(r.Context(), req.Vector, req.K)
	} else if req.Query != "" {
		docs, err = table.SearchVectorByText(r.Context(), req.Query, req.K)
	} else {
		writeErrorMsg(w, 400, "bad_request", "either vector or query is required")
		return
	}

	if err != nil {
		writeSearchError(w, err)
		return
	}

	writeJSON(w, 200, docs)
}

func (s *Server) handleTextSearch(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	table, err := s.registry.GetTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}

	var req textSearchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}

	results, total, err := table.SearchText(r.Context(), req.Query, req.Limit, req.Offset)
	if err != nil {
		writeSearchError(w, err)
		return
	}

	writeJSON(w, 200, map[string]any{
		"results": results,
		"total":   total,
	})
}

func (s *Server) handleAttributeSearch(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("table")

	table, err := s.registry.GetTable(r.Context(), tableName)
	if err != nil {
		writeErrorMsg(w, 404, "not_found", "table not found: "+tableName)
		return
	}

	var req attributeSearchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}

	results, err := table.SearchAttributes(r.Context(), req.Filters, req.Limit, req.Offset)
	if err != nil {
		writeSearchError(w, err)
		return
	}

	writeJSON(w, 200, results)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
