package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	autobotagent "github.com/russellhaering/wasmdb/internal/autobot/agent"
)

type chatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if s.chatManager == nil {
		writeErrorMsg(w, 503, "unavailable", "chat agent not configured (missing ANTHROPIC_API_KEY)")
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
	if req.SessionID == "" {
		writeErrorMsg(w, 400, "bad_request", "session_id is required")
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
	events := s.chatManager.StreamMessage(ctx, req.SessionID, req.Message)

	for evt := range events {
		var data []byte
		var eventType string

		switch evt.Type {
		case autobotagent.EventTextDelta:
			eventType = "text"
			data, _ = json.Marshal(map[string]string{"text": evt.Text})

		case autobotagent.EventToolCallStart:
			eventType = "tool_start"
			data, _ = json.Marshal(map[string]string{
				"tool": evt.ToolName,
				"id":   evt.ToolID,
			})

		case autobotagent.EventToolResult:
			eventType = "tool_result"
			d := map[string]any{
				"id":     evt.ToolID,
				"result": evt.ToolResult,
			}
			if evt.ToolIsError {
				d["error"] = true
			}
			data, _ = json.Marshal(d)

		case autobotagent.EventDone:
			eventType = "done"
			data = []byte("{}")

		case autobotagent.EventError:
			eventType = "error"
			errMsg := "unknown error"
			if evt.Error != nil {
				errMsg = evt.Error.Error()
			}
			data, _ = json.Marshal(map[string]string{"error": errMsg})
			slog.Error("chat stream error", "session", req.SessionID, "err", errMsg)
		}

		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(data))
		flusher.Flush()

		if evt.Type == autobotagent.EventDone || evt.Type == autobotagent.EventError {
			break
		}
	}
}
