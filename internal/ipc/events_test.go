package ipc

import (
	"encoding/json"
	"testing"
	"time"
)

// ─── MarshalEvent ─────────────────────────────────────────────────────────────

func TestMarshalEvent_TrailingNewline(t *testing.T) {
	e := Event{
		Type:      EventObservationCreated,
		Timestamp: time.Unix(0, 0).UTC(),
		Payload:   json.RawMessage(`{"id":1}`),
	}
	wire, err := MarshalEvent(e)
	if err != nil {
		t.Fatalf("MarshalEvent: %v", err)
	}
	if wire[len(wire)-1] != '\n' {
		t.Fatalf("expected trailing newline, got %q", string(wire))
	}
}

func TestMarshalEvent_RoundTrip(t *testing.T) {
	ts := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)
	payload, _ := MarshalPayload(ObservationPayload{
		ID:         42,
		SyncID:     "sync-abc",
		Title:      "test",
		Visibility: "work",
	})

	original := Event{
		Type:      EventObservationUpdated,
		Timestamp: ts,
		Payload:   payload,
	}

	wire, err := MarshalEvent(original)
	if err != nil {
		t.Fatalf("MarshalEvent: %v", err)
	}

	// Strip trailing newline before unmarshal.
	var got Event
	if err := json.Unmarshal(wire[:len(wire)-1], &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.Type != original.Type {
		t.Errorf("type: got %q, want %q", got.Type, original.Type)
	}
	if !got.Timestamp.Equal(original.Timestamp) {
		t.Errorf("timestamp: got %v, want %v", got.Timestamp, original.Timestamp)
	}

	var gotPayload ObservationPayload
	if err := json.Unmarshal(got.Payload, &gotPayload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if gotPayload.ID != 42 {
		t.Errorf("payload.ID: got %d, want 42", gotPayload.ID)
	}
	if gotPayload.SyncID != "sync-abc" {
		t.Errorf("payload.SyncID: got %q, want sync-abc", gotPayload.SyncID)
	}
}

// ─── MarshalPayload ───────────────────────────────────────────────────────────

func TestMarshalPayload_ObservationPayload(t *testing.T) {
	p := ObservationPayload{
		ID:           1,
		SyncID:       "s1",
		SessionID:    "sess1",
		Type:         "decision",
		Title:        "Auth architecture",
		Content:      "Use JWT",
		Workspace:    "myproject",
		Visibility:   "work",
		Organization: "glim",
		TopicKey:     "architecture/auth",
		References: []Reference{
			{Type: "file", Target: "internal/auth/auth.go"},
			{Type: "concept", Target: "jwt"},
		},
		CreatedAt: time.Unix(100, 0).UTC(),
		UpdatedAt: time.Unix(200, 0).UTC(),
	}

	raw, err := MarshalPayload(p)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	var got ObservationPayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != p.ID {
		t.Errorf("ID: got %d, want %d", got.ID, p.ID)
	}
	if got.Workspace != p.Workspace {
		t.Errorf("Workspace: got %q, want %q", got.Workspace, p.Workspace)
	}
	if got.Organization != p.Organization {
		t.Errorf("Organization: got %q, want %q", got.Organization, p.Organization)
	}
	if got.TopicKey != p.TopicKey {
		t.Errorf("TopicKey: got %q, want %q", got.TopicKey, p.TopicKey)
	}
	if len(got.References) != 2 {
		t.Fatalf("References: got %d, want 2", len(got.References))
	}
	if got.References[0].Type != "file" || got.References[0].Target != "internal/auth/auth.go" {
		t.Errorf("References[0]: got %+v", got.References[0])
	}
}

func TestMarshalPayload_DeletedPayload(t *testing.T) {
	p := ObservationDeletedPayload{ID: 99, SyncID: "del-sync"}
	raw, err := MarshalPayload(p)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	var got ObservationDeletedPayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != 99 || got.SyncID != "del-sync" {
		t.Errorf("got %+v", got)
	}
}

func TestMarshalPayload_SessionPayload(t *testing.T) {
	summary := "session summary"
	p := SessionPayload{
		ID:        "sess-123",
		Project:   "ancora",
		Directory: "/home/user/project",
		Summary:   &summary,
	}
	raw, err := MarshalPayload(p)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	var got SessionPayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != p.ID {
		t.Errorf("ID: got %q, want %q", got.ID, p.ID)
	}
	if got.Summary == nil || *got.Summary != summary {
		t.Errorf("Summary: got %v, want %q", got.Summary, summary)
	}
}

func TestMarshalPayload_SessionPayload_NilSummary(t *testing.T) {
	p := SessionPayload{
		ID:        "sess-no-summary",
		Project:   "proj",
		Directory: "/tmp",
		Summary:   nil,
	}
	raw, err := MarshalPayload(p)
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	var got SessionPayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Summary != nil {
		t.Errorf("expected nil Summary, got %v", got.Summary)
	}
}

// ─── EventType constants ──────────────────────────────────────────────────────

func TestEventTypeConstants(t *testing.T) {
	expected := map[EventType]string{
		EventObservationCreated: "observation.created",
		EventObservationUpdated: "observation.updated",
		EventObservationDeleted: "observation.deleted",
		EventSessionCreated:     "session.created",
		EventSessionEnded:       "session.ended",
	}
	for et, want := range expected {
		if string(et) != want {
			t.Errorf("EventType %q != %q", et, want)
		}
	}
}

// ─── Reference ───────────────────────────────────────────────────────────────

func TestReference_OmitEmptyInPayload(t *testing.T) {
	// When no references, omitempty must suppress the field entirely.
	p := ObservationPayload{
		ID:         1,
		Visibility: "work",
	}
	raw, _ := MarshalPayload(p)
	if contains(raw, "references") {
		t.Errorf("expected no 'references' key in JSON when slice is nil, got: %s", raw)
	}
}

func TestObservationPayload_OmitEmptyOptionals(t *testing.T) {
	p := ObservationPayload{
		ID:         2,
		Visibility: "work",
	}
	raw, _ := MarshalPayload(p)
	if contains(raw, `"workspace"`) {
		t.Errorf("expected no 'workspace' key when empty, got: %s", raw)
	}
	if contains(raw, `"organization"`) {
		t.Errorf("expected no 'organization' key when empty, got: %s", raw)
	}
	if contains(raw, `"topic_key"`) {
		t.Errorf("expected no 'topic_key' key when empty, got: %s", raw)
	}
}

func contains(raw []byte, sub string) bool {
	return len(raw) > 0 && indexBytes(raw, []byte(sub)) >= 0
}

func indexBytes(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return i
		}
	}
	return -1
}
