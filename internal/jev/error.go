package jev

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// APIError is a non-2xx response. It never carries the request body or the API
// key.
type APIError struct {
	Status     int
	Message    string
	RequestID  string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("jev: %d %s", e.Status, e.Message)
	if e.RequestID != "" {
		msg += " (request " + e.RequestID + ")"
	}
	return msg
}

// Retryable reports whether the same request may succeed on another attempt.
func (e *APIError) Retryable() bool {
	return e.Status == http.StatusRequestTimeout ||
		e.Status == http.StatusTooManyRequests ||
		e.Status >= 500
}

// errorMessage digs the human-readable part out of a response body, trying the
// three shapes the API uses before giving up on the status line.
func errorMessage(body []byte, status int) string {
	var envelope struct {
		Error   json.RawMessage `json:"error"`
		Message json.RawMessage `json:"message"`
		Detail  json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		for _, field := range []json.RawMessage{envelope.Error, envelope.Message, envelope.Detail} {
			if msg := decodeMessage(field); msg != "" {
				return msg
			}
		}
	}
	if text := http.StatusText(status); text != "" {
		return text
	}
	return "unknown error"
}

func decodeMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var object struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &object) == nil && object.Message != "" {
		return strings.TrimSpace(object.Message)
	}
	var items []struct {
		Loc []any  `json:"loc"`
		Msg string `json:"msg"`
	}
	if json.Unmarshal(raw, &items) == nil {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			if item.Msg == "" {
				continue
			}
			if loc := joinLoc(item.Loc); loc != "" {
				parts = append(parts, loc+": "+item.Msg)
				continue
			}
			parts = append(parts, item.Msg)
		}
		return strings.Join(parts, "; ")
	}
	return ""
}

func joinLoc(loc []any) string {
	parts := make([]string, 0, len(loc))
	for _, p := range loc {
		switch v := p.(type) {
		case string:
			parts = append(parts, v)
		case float64:
			parts = append(parts, fmt.Sprintf("%d", int64(v)))
		default:
			parts = append(parts, fmt.Sprint(v))
		}
	}
	return strings.Join(parts, ".")
}
