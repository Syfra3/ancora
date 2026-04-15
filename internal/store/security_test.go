package store

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestIsValidSQLIdentifier verifies strict identifier validation to prevent SQL injection.
func TestIsValidSQLIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid simple", "users", true},
		{"valid underscore", "user_profiles", true},
		{"valid starts underscore", "_internal", true},
		{"valid mixed case", "UserProfiles", true},
		{"valid numbers", "table123", true},
		{"empty", "", false},
		{"starts with number", "123table", false},
		{"contains space", "user profiles", false},
		{"contains dash", "user-profiles", false},
		{"contains semicolon", "users;", false},
		{"contains quote", "users'", false},
		{"contains backtick", "users`", false},
		{"contains parenthesis", "users()", false},
		{"SQL keyword injection", "users; DROP TABLE", false},
		{"too long", strings.Repeat("a", 129), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidSQLIdentifier(tt.input)
			if got != tt.want {
				t.Errorf("isValidSQLIdentifier(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestAddColumnIfNotExistsRejectsInvalidIdentifiers ensures SQL injection prevention.
func TestAddColumnIfNotExistsRejectsInvalidIdentifiers(t *testing.T) {
	s := newTestStore(t)

	// Create a valid test table
	if _, err := s.db.Exec(`CREATE TABLE test_table (id INTEGER)`); err != nil {
		t.Fatalf("create test table: %v", err)
	}

	tests := []struct {
		name      string
		tableName string
		colName   string
		wantErr   bool
	}{
		{"valid identifiers", "test_table", "new_col", false},
		{"invalid table name", "test_table; DROP TABLE users", "col", true},
		{"invalid column name", "test_table", "col'; DROP TABLE", true},
		{"table with quote", "test'table", "col", true},
		{"column with semicolon", "test_table", "col;", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.addColumnIfNotExists(tt.tableName, tt.colName, "TEXT")
			if (err != nil) != tt.wantErr {
				t.Errorf("addColumnIfNotExists(%q, %q) error = %v, wantErr %v",
					tt.tableName, tt.colName, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "invalid") {
				t.Errorf("expected 'invalid' in error, got: %v", err)
			}
		})
	}
}

// TestTruncateUTF8WithSuffix ensures final length never exceeds maxBytes.
func TestTruncateUTF8WithSuffix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
		suffix   string
	}{
		{
			name:     "ASCII within limit",
			input:    "hello",
			maxBytes: 20,
			suffix:   "...",
		},
		{
			name:     "ASCII needs truncation",
			input:    "hello world this is a long string",
			maxBytes: 20,
			suffix:   "... [truncated]",
		},
		{
			name:     "UTF-8 multibyte",
			input:    "hello 世界 world",
			maxBytes: 20,
			suffix:   "...",
		},
		{
			name:     "suffix too large",
			input:    "hello",
			maxBytes: 3,
			suffix:   "... [very long suffix]",
		},
		{
			name:     "exact boundary",
			input:    "12345",
			maxBytes: 8,
			suffix:   "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateUTF8(tt.input, tt.maxBytes, tt.suffix)

			// Critical invariant: result must never exceed maxBytes
			if len(result) > tt.maxBytes {
				t.Errorf("truncateUTF8() result length %d exceeds maxBytes %d\nresult: %q",
					len(result), tt.maxBytes, result)
			}

			// Result must end with suffix if input was truncated
			if len(tt.input) > tt.maxBytes && !strings.HasSuffix(result, tt.suffix) {
				// Exception: suffix itself may be truncated if too large
				if len(tt.suffix) < tt.maxBytes {
					t.Errorf("truncateUTF8() result should end with suffix\ngot: %q\nsuffix: %q",
						result, tt.suffix)
				}
			}

			// Result must be valid UTF-8
			if !utf8.ValidString(result) {
				t.Errorf("truncateUTF8() produced invalid UTF-8: %q", result)
			}
		})
	}
}

// TestSearchTopicKeyErrorHandling verifies error propagation instead of silent breaks.
func TestSearchTopicKeyErrorHandling(t *testing.T) {
	s := newTestStore(t)

	// Create test data
	if err := s.CreateSession("s1", "test", "/tmp"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	obsID, err := s.AddObservation(AddObservationParams{
		SessionID:  "s1",
		Type:       "manual",
		Title:      "Test",
		Content:    "Test content",
		TopicKey:   "test/topic",
		Workspace:  "test",
		Visibility: "work",
	})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}

	// Valid search should work
	results, err := s.Search("test/topic", SearchOptions{})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 || results[0].ID != obsID {
		t.Errorf("expected 1 result with ID %d, got %d results", obsID, len(results))
	}

	// Test with query hook that simulates error
	origHook := s.hooks.queryIt
	s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
		if strings.Contains(query, "topic_key") {
			return nil, errors.New("simulated query error")
		}
		return origHook(db, query, args...)
	}
	defer func() { s.hooks.queryIt = origHook }()

	// Search should return error instead of empty results
	_, err = s.Search("test/topic", SearchOptions{})
	if err == nil {
		t.Error("expected error when topic_key query fails, got nil")
	}
	if !strings.Contains(err.Error(), "topic_key") {
		t.Errorf("expected 'topic_key' in error, got: %v", err)
	}
}

// TestStatsErrorHandling verifies proper error propagation in Stats().
func TestStatsErrorHandling(t *testing.T) {
	s := newTestStore(t)

	// Valid Stats() should work
	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}

	// Test with query hook that simulates scan error
	origHook := s.hooks.queryIt
	s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
		if strings.Contains(query, "workspace") {
			return &fakeRows{
				next:    []bool{true},
				scanErr: errors.New("simulated scan error"),
			}, nil
		}
		return origHook(db, query, args...)
	}

	_, err = s.Stats()
	if err == nil {
		t.Error("expected error when workspace scan fails, got nil")
	}
	if !strings.Contains(err.Error(), "scan workspace") {
		t.Errorf("expected 'scan workspace' in error, got: %v", err)
	}

	// Test with iteration error
	s.hooks.queryIt = func(db queryer, query string, args ...any) (rowScanner, error) {
		if strings.Contains(query, "workspace") {
			return &fakeRows{
				next: []bool{false},
				err:  errors.New("simulated iteration error"),
			}, nil
		}
		return origHook(db, query, args...)
	}

	_, err = s.Stats()
	if err == nil {
		t.Error("expected error when workspace iteration fails, got nil")
	}
	if !strings.Contains(err.Error(), "iterate workspaces") {
		t.Errorf("expected 'iterate workspaces' in error, got: %v", err)
	}

	s.hooks.queryIt = origHook
}
