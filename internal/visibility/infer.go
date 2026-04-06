package visibility

import "strings"

// InferVisibility analyzes title and content to determine if the observation
// is personal or work-related. Returns "personal" or "work".
//
// Personal triggers include: my goals, my health, my finances, personal, private, etc.
// Work triggers include: bug, feature, API, database, refactor, etc.
// Default: work (if no personal triggers found)
func InferVisibility(title, content string) string {
	// Personal keywords from MIGRATION_PLAN.md
	personalTriggers := []string{
		// Explicit personal markers
		"my goals", "my health", "my weight", "my finances",
		"my salary", "my budget", "personal", "private",
		"family", "home", "vacation", "medical",

		// Body/health related
		"body measurement", "body weight", "blood pressure",
		"cholesterol", "doctor visit", "prescription",

		// Financial
		"bank account", "credit card", "mortgage", "loan payment",
		"investment", "401k", "retirement",

		// Life events
		"birthday", "anniversary", "wedding", "funeral",
	}

	// Combine title and content for analysis (title has higher priority)
	combined := strings.ToLower(title + " " + content)

	// Check for personal triggers
	for _, trigger := range personalTriggers {
		if strings.Contains(combined, trigger) {
			return "personal"
		}
	}

	// Default to work if no personal triggers found
	return "work"
}
