package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	autobotagent "github.com/russellhaering/wasmdb/internal/autobot/agent"
)

type chatRequest struct {
	SessionID string `json:"session_id"` // empty => server generates a new one
	Message   string `json:"message"`
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if s.chatManager == nil {
		writeErrorMsg(w, 503, "unavailable", "chat agent not configured (missing ANTHROPIC_API_KEY)")
		return
	}

	session := SessionFromContext(r.Context())
	if session == nil {
		writeError(w, ErrUnauthorized)
		return
	}

	var req chatRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorMsg(w, 400, "bad_request", "invalid JSON: "+err.Error())
		return
	}

	if req.Message == "" {
		writeErrorMsg(w, 400, "bad_request", "message is required")
		return
	}

	// Set up SSE.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorMsg(w, 500, "internal_error", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()
	sessionID, events := s.chatManager.StreamMessage(ctx, req.SessionID, session.UserID, req.Message)

	// Always send session_id as the first event so the client tracks it.
	sidData, _ := json.Marshal(map[string]string{"session_id": sessionID})
	fmt.Fprintf(w, "event: session\ndata: %s\n\n", string(sidData))
	flusher.Flush()

	// Helper to send an SSE event.
	sendSSE := func(eventType string, data []byte) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(data))
		flusher.Flush()
	}

	// Text deltas stream directly as "text" events. Surface pages are embedded
	// by reference (```surface-ref fences the client parses on completion), so
	// there is no server-side artifact splitting anymore.
	sendText := func(text string) {
		if text == "" {
			return
		}
		d, _ := json.Marshal(map[string]string{"text": text})
		sendSSE("text", d)
	}

	for evt := range events {
		switch evt.Type {
		case autobotagent.EventTextDelta:
			sendText(evt.Text)

		case autobotagent.EventToolCallStart:
			d := map[string]any{
				"tool": evt.ToolName,
				"id":   evt.ToolID,
			}
			if len(evt.ToolInput) > 0 {
				var input any
				if err := json.Unmarshal(evt.ToolInput, &input); err == nil {
					d["input"] = input
				}
			}
			data, _ := json.Marshal(d)
			sendSSE("tool_start", data)

		case autobotagent.EventToolResult:
			d := map[string]any{
				"id":     evt.ToolID,
				"result": evt.ToolResult,
			}
			if evt.ToolName != "" {
				d["tool"] = evt.ToolName
			}
			if evt.ToolIsError {
				d["error"] = true
			}
			data, _ := json.Marshal(d)
			sendSSE("tool_result", data)

		case autobotagent.EventDone:
			sendSSE("done", []byte("{}"))

		case autobotagent.EventError:
			errMsg := "unknown error"
			if evt.Error != nil {
				errMsg = evt.Error.Error()
			}
			data, _ := json.Marshal(map[string]string{"error": errMsg})
			slog.Error("chat stream error", "session", sessionID, "err", errMsg)
			sendSSE("error", data)
		}

		if evt.Type == autobotagent.EventDone || evt.Type == autobotagent.EventError {
			break
		}
	}
}

func (s *Server) handleListChatSessions(w http.ResponseWriter, r *http.Request) {
	if s.chatManager == nil {
		writeErrorMsg(w, 503, "unavailable", "chat agent not configured")
		return
	}

	session := SessionFromContext(r.Context())
	if session == nil {
		writeError(w, ErrUnauthorized)
		return
	}

	sessions, err := s.chatManager.ListSessions(r.Context(), session.UserID)
	if err != nil {
		slog.Error("failed to list chat sessions", "err", err)
		writeErrorMsg(w, 500, "internal_error", "failed to list sessions")
		return
	}

	writeJSON(w, 200, map[string]any{"sessions": sessions})
}

func (s *Server) handleDeleteChatSession(w http.ResponseWriter, r *http.Request) {
	if s.chatManager == nil {
		writeErrorMsg(w, 503, "unavailable", "chat agent not configured")
		return
	}

	session := SessionFromContext(r.Context())
	if session == nil {
		writeError(w, ErrUnauthorized)
		return
	}

	csID := r.PathValue("id")
	if csID == "" {
		writeErrorMsg(w, 400, "bad_request", "session id is required")
		return
	}

	if err := s.chatManager.DeleteSession(r.Context(), csID, session.UserID); err != nil {
		slog.Error("failed to delete chat session", "session", csID, "err", err)
		writeErrorMsg(w, 404, "not_found", "session not found")
		return
	}

	w.WriteHeader(204)
}
