package store

// Tests for IPC event emission hooks in Store.
// Uses a fake EventEmitter (no OS socket needed) to verify that
// AddObservation, UpdateObservation, DeleteObservation, CreateSession,
// and EndSession each fire the expected event types.

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/Syfra3/ancora/internal/ipc"
)

// ─── Fake EventEmitter ────────────────────────────────────────────────────────

type captureEmitter struct {
	mu     sync.Mutex
	events []ipc.Event
}

func (c *captureEmitter) Emit(e ipc.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *captureEmitter) received() []ipc.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ipc.Event, len(c.events))
	copy(out, c.events)
	return out
}

func (c *captureEmitter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func newTestStoreWithEmitter(t *testing.T) (*Store, *captureEmitter) {
	t.Helper()
	s := newTestStore(t)
	em := &captureEmitter{}
	s.SetEventServer(em)
	return s, em
}

func mustCreateSession(t *testing.T, s *Store, id, project, dir string) {
	t.Helper()
	if err := s.CreateSession(id, project, dir); err != nil {
		t.Fatalf("CreateSession(%q): %v", id, err)
	}
}

func mustAddObservation(t *testing.T, s *Store, p AddObservationParams) int64 {
	t.Helper()
	id, err := s.AddObservation(p)
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	return id
}

// ─── SetEventServer nil guard ─────────────────────────────────────────────────

func TestStore_Emit_NoServer_NoOp(t *testing.T) {
	// Store without event server — writes must not panic.
	s := newTestStore(t)
	mustCreateSession(t, s, "s1", "ancora", "/tmp")
	mustAddObservation(t, s, AddObservationParams{
		SessionID:  "s1",
		Type:       "decision",
		Title:      "no server",
		Content:    "should not panic",
		Visibility: "work",
	})
}

// ─── observation.created ──────────────────────────────────────────────────────

func TestStore_Emit_ObservationCreated(t *testing.T) {
	s, em := newTestStoreWithEmitter(t)
	mustCreateSession(t, s, "s1", "ancora", "/tmp")
	em.reset() // discard session.created events

	id := mustAddObservation(t, s, AddObservationParams{
		SessionID:  "s1",
		Type:       "decision",
		Title:      "Emit test",
		Content:    "IPC test content",
		Visibility: "work",
		Workspace:  "myproject",
	})

	events := em.received()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Type != ipc.EventObservationCreated {
		t.Errorf("type: got %q, want %q", e.Type, ipc.EventObservationCreated)
	}

	var payload ipc.ObservationPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.ID != id {
		t.Errorf("payload.ID: got %d, want %d", payload.ID, id)
	}
	if payload.Title != "Emit test" {
		t.Errorf("payload.Title: got %q", payload.Title)
	}
	if payload.Workspace != "myproject" {
		t.Errorf("payload.Workspace: got %q", payload.Workspace)
	}
	if payload.Visibility != "work" {
		t.Errorf("payload.Visibility: got %q", payload.Visibility)
	}
}

func TestStore_Emit_ObservationCreated_WithReferences(t *testing.T) {
	s, em := newTestStoreWithEmitter(t)
	mustCreateSession(t, s, "s1", "ancora", "/tmp")
	em.reset()

	refs := `[{"type":"file","target":"internal/store/store.go"},{"type":"concept","target":"jwt"}]`
	mustAddObservation(t, s, AddObservationParams{
		SessionID:  "s1",
		Type:       "architecture",
		Title:      "Auth design",
		Content:    "JWT for auth",
		Visibility: "work",
		References: refs,
	})

	events := em.received()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	var payload ipc.ObservationPayload
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.References) != 2 {
		t.Fatalf("References: got %d, want 2", len(payload.References))
	}
	if payload.References[0].Type != "file" || payload.References[0].Target != "internal/store/store.go" {
		t.Errorf("References[0]: got %+v", payload.References[0])
	}
	if payload.References[1].Type != "concept" || payload.References[1].Target != "jwt" {
		t.Errorf("References[1]: got %+v", payload.References[1])
	}
}

// ─── observation.updated ──────────────────────────────────────────────────────

func TestStore_Emit_ObservationUpdated(t *testing.T) {
	s, em := newTestStoreWithEmitter(t)
	mustCreateSession(t, s, "s1", "ancora", "/tmp")
	id := mustAddObservation(t, s, AddObservationParams{
		SessionID:  "s1",
		Type:       "decision",
		Title:      "Original title",
		Content:    "Original content",
		Visibility: "work",
	})
	em.reset()

	newTitle := "Updated title"
	newContent := "Updated content"
	if _, err := s.UpdateObservation(id, UpdateObservationParams{
		Title:   &newTitle,
		Content: &newContent,
	}); err != nil {
		t.Fatalf("UpdateObservation: %v", err)
	}

	events := em.received()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Type != ipc.EventObservationUpdated {
		t.Errorf("type: got %q, want %q", e.Type, ipc.EventObservationUpdated)
	}

	var payload ipc.ObservationPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.ID != id {
		t.Errorf("payload.ID: got %d, want %d", payload.ID, id)
	}
	if payload.Title != newTitle {
		t.Errorf("payload.Title: got %q, want %q", payload.Title, newTitle)
	}
}

func TestStore_Emit_ObservationUpdated_WithReferences(t *testing.T) {
	s, em := newTestStoreWithEmitter(t)
	mustCreateSession(t, s, "s1", "ancora", "/tmp")
	id := mustAddObservation(t, s, AddObservationParams{
		SessionID:  "s1",
		Type:       "decision",
		Title:      "No refs initially",
		Content:    "content",
		Visibility: "work",
	})
	em.reset()

	refs := `[{"type":"observation","target":"42"}]`
	if _, err := s.UpdateObservation(id, UpdateObservationParams{
		References: &refs,
	}); err != nil {
		t.Fatalf("UpdateObservation: %v", err)
	}

	events := em.received()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	var payload ipc.ObservationPayload
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.References) != 1 || payload.References[0].Type != "observation" {
		t.Errorf("References: got %+v", payload.References)
	}
}

// ─── observation.deleted ──────────────────────────────────────────────────────

func TestStore_Emit_ObservationDeleted_SoftDelete(t *testing.T) {
	s, em := newTestStoreWithEmitter(t)
	mustCreateSession(t, s, "s1", "ancora", "/tmp")
	id := mustAddObservation(t, s, AddObservationParams{
		SessionID:  "s1",
		Type:       "decision",
		Title:      "To delete",
		Content:    "content",
		Visibility: "work",
	})
	em.reset()

	if err := s.DeleteObservation(id, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}

	events := em.received()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Type != ipc.EventObservationDeleted {
		t.Errorf("type: got %q, want %q", e.Type, ipc.EventObservationDeleted)
	}

	var payload ipc.ObservationDeletedPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.ID != id {
		t.Errorf("payload.ID: got %d, want %d", payload.ID, id)
	}
	if payload.SyncID == "" {
		t.Errorf("payload.SyncID must not be empty")
	}
}

func TestStore_Emit_ObservationDeleted_HardDelete(t *testing.T) {
	s, em := newTestStoreWithEmitter(t)
	mustCreateSession(t, s, "s1", "ancora", "/tmp")
	id := mustAddObservation(t, s, AddObservationParams{
		SessionID:  "s1",
		Type:       "decision",
		Title:      "Hard delete target",
		Content:    "content",
		Visibility: "work",
	})
	em.reset()

	if err := s.DeleteObservation(id, true); err != nil {
		t.Fatalf("DeleteObservation (hard): %v", err)
	}

	events := em.received()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != ipc.EventObservationDeleted {
		t.Errorf("type: got %q, want %q", events[0].Type, ipc.EventObservationDeleted)
	}
}

// ─── session.created ──────────────────────────────────────────────────────────

func TestStore_Emit_SessionCreated(t *testing.T) {
	s, em := newTestStoreWithEmitter(t)

	if err := s.CreateSession("sess-emit", "myproject", "/work"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	events := em.received()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Type != ipc.EventSessionCreated {
		t.Errorf("type: got %q, want %q", e.Type, ipc.EventSessionCreated)
	}

	var payload ipc.SessionPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.ID != "sess-emit" {
		t.Errorf("payload.ID: got %q", payload.ID)
	}
	if payload.Project != "myproject" {
		t.Errorf("payload.Project: got %q", payload.Project)
	}
	if payload.Directory != "/work" {
		t.Errorf("payload.Directory: got %q", payload.Directory)
	}
}

// ─── session.ended ────────────────────────────────────────────────────────────

func TestStore_Emit_SessionEnded(t *testing.T) {
	s, em := newTestStoreWithEmitter(t)
	mustCreateSession(t, s, "sess-end", "ancora", "/tmp")
	em.reset()

	summary := "session summary text"
	if err := s.EndSession("sess-end", summary); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	events := em.received()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.Type != ipc.EventSessionEnded {
		t.Errorf("type: got %q, want %q", e.Type, ipc.EventSessionEnded)
	}

	var payload ipc.SessionPayload
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.ID != "sess-end" {
		t.Errorf("payload.ID: got %q", payload.ID)
	}
	if payload.Summary == nil || *payload.Summary != summary {
		t.Errorf("payload.Summary: got %v, want %q", payload.Summary, summary)
	}
}

func TestStore_Emit_SessionEnded_NilSummary(t *testing.T) {
	s, em := newTestStoreWithEmitter(t)
	mustCreateSession(t, s, "sess-nosum", "ancora", "/tmp")
	em.reset()

	if err := s.EndSession("sess-nosum", ""); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	events := em.received()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	var payload ipc.SessionPayload
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Empty summary string — payload.Summary may be nil or point to empty string.
	if payload.Summary != nil && *payload.Summary != "" {
		t.Errorf("expected empty Summary, got %q", *payload.Summary)
	}
}

// ─── Timestamp not zero ───────────────────────────────────────────────────────

func TestStore_Emit_TimestampSet(t *testing.T) {
	s, em := newTestStoreWithEmitter(t)
	mustCreateSession(t, s, "s-ts", "ancora", "/tmp")
	em.reset()

	mustAddObservation(t, s, AddObservationParams{
		SessionID:  "s-ts",
		Type:       "decision",
		Title:      "ts check",
		Content:    "content",
		Visibility: "work",
	})

	events := em.received()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Timestamp.IsZero() {
		t.Errorf("event timestamp must not be zero")
	}
}

// ─── Multiple events in sequence ─────────────────────────────────────────────

func TestStore_Emit_MultipleOperationsSequence(t *testing.T) {
	s, em := newTestStoreWithEmitter(t)

	// CreateSession -> observation.created -> observation.updated -> observation.deleted
	mustCreateSession(t, s, "s-seq", "ancora", "/tmp")

	id := mustAddObservation(t, s, AddObservationParams{
		SessionID:  "s-seq",
		Type:       "decision",
		Title:      "seq",
		Content:    "c",
		Visibility: "work",
	})

	newContent := "updated"
	if _, err := s.UpdateObservation(id, UpdateObservationParams{Content: &newContent}); err != nil {
		t.Fatalf("UpdateObservation: %v", err)
	}

	if err := s.DeleteObservation(id, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}

	events := em.received()
	// session.created + observation.created + observation.updated + observation.deleted
	if len(events) != 4 {
		types := make([]string, len(events))
		for i, e := range events {
			types[i] = string(e.Type)
		}
		t.Fatalf("expected 4 events, got %d: %v", len(events), types)
	}

	expected := []ipc.EventType{
		ipc.EventSessionCreated,
		ipc.EventObservationCreated,
		ipc.EventObservationUpdated,
		ipc.EventObservationDeleted,
	}
	for i, want := range expected {
		if events[i].Type != want {
			t.Errorf("event[%d]: got %q, want %q", i, events[i].Type, want)
		}
	}
}
