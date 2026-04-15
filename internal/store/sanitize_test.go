package store

import (
	"testing"
)

func TestSanitizeFTS_EmbeddedQuotes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no quotes",
			input: "fix auth bug",
			want:  `"fix" "auth" "bug"`,
		},
		{
			name:  "apostrophe - not a double quote, passes through",
			input: `user's data`,
			want:  `"user's" "data"`,
		},
		{
			name:  "embedded double quotes - should be escaped",
			input: `test"value OR 1=1`,
			want:  `"test""value" "OR" "1=1"`,
		},
		{
			name:  "word wrapped in quotes - Fields strips them, safe",
			input: `test "embedded" word`,
			want:  `"test" "embedded" "word"`,
		},
		{
			name:  "edge case - just a double quote - becomes empty token",
			input: `"`,
			want:  `""`,
		},
		{
			name:  "injection attempt - OR operator broken into separate token",
			input: `test" OR 1=1`,
			want:  `"test" "OR" "1=1"`,
		},
		{
			name:  "multiple double quotes in one word",
			input: `a"b"c`,
			want:  `"a""b""c"`,
		},
		{
			name:  "injection with leading quote",
			input: `" OR 1=1 --`,
			want:  `"" "OR" "1=1" "--"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFTS(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFTS(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
