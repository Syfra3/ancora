package visibility

import "testing"

func TestInferVisibility_Personal(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		content string
	}{
		{"My goals", "2026 Goals", "my goals for the year"},
		{"My health", "Health Update", "my health checkup went well"},
		{"My finances", "Budget Review", "reviewing my finances this month"},
		{"My salary", "Salary Negotiation", "my salary increased by 10%"},
		{"My budget", "Monthly Budget", "my budget for groceries"},
		{"Personal notes", "Personal Reflection", "some personal thoughts"},
		{"Private data", "Private Notes", "this is private information"},
		{"Family event", "Family Dinner", "family gathering this weekend"},
		{"Home project", "Home Renovation", "home improvement project started"},
		{"Vacation plans", "Summer Vacation", "planning vacation to Italy"},
		{"Medical record", "Medical Appointment", "medical checkup scheduled"},
		{"Body measurement", "Weight Tracking", "body measurement: 180 lbs"},
		{"Body weight", "Weight Loss", "tracking body weight progress"},
		{"Blood pressure", "Health Metrics", "blood pressure: 120/80"},
		{"Doctor visit", "Checkup", "doctor visit went well"},
		{"Bank account", "Banking", "bank account balance low"},
		{"Credit card", "Finances", "credit card payment due"},
		{"Investment", "Portfolio", "investment portfolio review"},
		{"Birthday", "Birthday Party", "birthday celebration planning"},
		{"Title priority", "my health", "this talks about API and bug fixes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferVisibility(tt.title, tt.content)
			if got != "personal" {
				t.Errorf("InferVisibility(%q, %q) = %q; want %q", tt.title, tt.content, got, "personal")
			}
		})
	}
}

func TestInferVisibility_Work(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		content string
	}{
		{"Bug fix", "Fixed auth bug", "bug in authentication middleware"},
		{"Feature", "Add feature X", "feature implementation complete"},
		{"API change", "API Update", "API endpoint refactored"},
		{"Database", "DB Migration", "database schema updated"},
		{"Refactor", "Code Refactor", "refactor authentication logic"},
		{"Architecture", "System Design", "architecture decision made"},
		{"Config", "Configuration Update", "config file updated"},
		{"Performance", "Performance Fix", "performance optimization"},
		{"Security", "Security Patch", "security vulnerability fixed"},
		{"Testing", "Test Coverage", "testing suite improved"},
		{"Documentation", "Docs Update", "documentation revised"},
		{"Build", "Build Fix", "build pipeline fixed"},
		{"Deployment", "Deploy to prod", "deployment completed"},
		{"Empty", "", ""},
		{"Generic work", "Work Notes", "some work-related notes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferVisibility(tt.title, tt.content)
			if got != "work" {
				t.Errorf("InferVisibility(%q, %q) = %q; want %q", tt.title, tt.content, got, "work")
			}
		})
	}
}

func TestInferVisibility_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		content string
		want    string
	}{
		{"Empty both", "", "", "work"},
		{"Whitespace only", "   ", "   ", "work"},
		{"Title only personal", "my health", "", "personal"},
		{"Content only personal", "", "checking my finances", "personal"},
		{"Mixed content", "API Design", "bug fix and my health notes", "personal"}, // personal wins
		{"Case insensitive", "MY GOALS", "TRACKING MY BUDGET", "personal"},
		{"Partial match", "project scope", "work on the project", "work"},               // "project" is not "my project"
		{"Work with personal words", "Private API", "api keys are private", "personal"}, // "private" triggers
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferVisibility(tt.title, tt.content)
			if got != tt.want {
				t.Errorf("InferVisibility(%q, %q) = %q; want %q", tt.title, tt.content, got, tt.want)
			}
		})
	}
}

func TestInferVisibility_TitlePriority(t *testing.T) {
	// Title has same weight as content in our implementation
	// Personal trigger anywhere in title+content should classify as personal
	title := "my health update"
	content := "fixed API bug, refactored database, improved performance"

	got := InferVisibility(title, content)
	if got != "personal" {
		t.Errorf("InferVisibility with personal title should return 'personal', got %q", got)
	}
}
