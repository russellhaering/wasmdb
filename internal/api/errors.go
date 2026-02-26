package api

import (
	"encoding/json"
	"net/http"
)

// APIError represents a structured API error response.
type APIError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return e.Message
}

var (
	ErrUnauthorized   = &APIError{Status: 401, Code: "unauthorized", Message: "authentication required"}
	ErrNotFound       = &APIError{Status: 404, Code: "not_found", Message: "resource not found"}
	ErrBadRequest     = &APIError{Status: 400, Code: "bad_request", Message: "invalid request"}
	ErrConflict       = &APIError{Status: 409, Code: "conflict", Message: "resource already exists"}
	ErrInternalServer = &APIError{Status: 500, Code: "internal_error", Message: "internal server error"}
)

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a structured error response.
func writeError(w http.ResponseWriter, err *APIError) {
	writeJSON(w, err.Status, err)
}

// writeErrorMsg writes an error with a custom message.
func writeErrorMsg(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, &APIError{Code: code, Message: message})
}

// decodeJSON decodes a JSON request body into v.
func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
