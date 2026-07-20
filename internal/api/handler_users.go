package api

import (
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/russellhaering/moraine/document"
)

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   uint64    `json:"version"`
}

func userResponseFromDoc(doc *document.Document) userResponse {
	email, _ := doc.Attributes["email"].(string)
	return userResponse{
		ID:        doc.ID,
		Email:     email,
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
		Version:   doc.Version,
	}
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	if req.Email == "" {
		writeErrorMsg(w, 400, "bad_request", "email is required")
		return
	}
	if req.Password == "" {
		writeErrorMsg(w, 400, "bad_request", "password is required")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", "failed to hash password")
		return
	}

	table, err := s.registry.GetTable(r.Context(), "_users")
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", "users table not available")
		return
	}

	// Enforce email uniqueness by scanning all users.
	afterKey := ""
	for {
		pageUsers, hasMore, listErr := table.ListDocuments(r.Context(), 100, afterKey)
		if listErr != nil {
			writeErrorMsg(w, 500, "internal_error", "failed to check email uniqueness")
			return
		}
		for _, u := range pageUsers {
			if e, _ := u.Attributes["email"].(string); e == req.Email {
				writeErrorMsg(w, 409, "conflict", "a user with this email already exists")
				return
			}
		}
		if !hasMore || len(pageUsers) == 0 {
			break
		}
		afterKey = pageUsers[len(pageUsers)-1].ID
	}

	doc := &document.Document{
		Attributes: map[string]any{
			"email":         req.Email,
			"password_hash": string(hash),
		},
	}

	if err := table.PutDocument(r.Context(), doc); err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	writeJSON(w, 201, userResponseFromDoc(doc))
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	table, err := s.registry.GetTable(r.Context(), "_users")
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", "users table not available")
		return
	}

	docs, _, err := table.ListDocuments(r.Context(), 1000, "")
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	users := make([]userResponse, len(docs))
	for i, doc := range docs {
		users[i] = userResponseFromDoc(doc)
	}
	writeJSON(w, 200, users)
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	table, err := s.registry.GetTable(r.Context(), "_users")
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", "users table not available")
		return
	}

	doc, err := table.GetDocument(r.Context(), id)
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}
	if doc == nil {
		writeErrorMsg(w, 404, "not_found", "user not found")
		return
	}

	writeJSON(w, 200, userResponseFromDoc(doc))
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	table, err := s.registry.GetTable(r.Context(), "_users")
	if err != nil {
		writeErrorMsg(w, 500, "internal_error", "users table not available")
		return
	}

	if err := table.DeleteDocument(r.Context(), id); err != nil {
		writeErrorMsg(w, 500, "internal_error", err.Error())
		return
	}

	// Clean up user's sessions.
	_ = s.sessions.DeleteUserSessions(r.Context(), id)

	w.WriteHeader(204)
}
