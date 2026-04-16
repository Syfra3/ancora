package ipc

import (
	"encoding/json"
	"time"
)

// ─── Event Types ──────────────────────────────────────────────────────────────

// EventType identifies the kind of change that occurred.
type EventType string

const (
	EventObservationCreated EventType = "observation.created"
	EventObservationUpdated EventType = "observation.updated"
	EventObservationDeleted EventType = "observation.deleted"
	EventSessionCreated     EventType = "session.created"
	EventSessionEnded       EventType = "session.ended"
)

// Event is the top-level envelope transmitted over the IPC socket.
// Events are serialised as NDJSON (one JSON object per line).
type Event struct {
	Type      EventType       `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// ─── Observation Payloads ─────────────────────────────────────────────────────

// Reference describes a link from an observation to another entity.
type Reference struct {
	// Type is one of "file", "observation", "concept", "function".
	Type string `json:"type"`
	// Target is the referenced entity: a file path, observation ID string,
	// concept name, or function/struct name.
	Target string `json:"target"`
}

// ObservationPayload is sent for observation.created and observation.updated.
type ObservationPayload struct {
	ID         int64       `json:"id"`
	SyncID     string      `json:"sync_id"`
	SessionID  string      `json:"session_id"`
	Type       string      `json:"type"`
	Title      string      `json:"title"`
	Content    string      `json:"content"`
	Workspace  string      `json:"workspace,omitempty"`
	Visibility string      `json:"visibility"`
	TopicKey   string      `json:"topic_key,omitempty"`
	References []Reference `json:"references,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// ObservationDeletedPayload is sent for observation.deleted.
type ObservationDeletedPayload struct {
	ID     int64  `json:"id"`
	SyncID string `json:"sync_id"`
}

// SessionPayload is sent for session.created and session.ended.
type SessionPayload struct {
	ID        string  `json:"id"`
	Project   string  `json:"project"`
	Directory string  `json:"directory"`
	Summary   *string `json:"summary,omitempty"`
}

// ─── Wire Protocol ────────────────────────────────────────────────────────────

// MarshalEvent serialises an Event to its NDJSON wire representation
// (JSON bytes followed by a newline character).
func MarshalEvent(e Event) ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// MarshalPayload is a convenience helper that marshals any payload value
// into a json.RawMessage suitable for Event.Payload.
func MarshalPayload(v any) (json.RawMessage, error) {
	return json.Marshal(v)
}
