package classify

import (
	"strings"
	"sync"

	"github.com/Syfra3/ancora/internal/store"
)

// WorkspaceLister is the minimal interface the Classifier needs from the store.
// Satisfied by *store.Store.
type WorkspaceLister interface {
	ListProjectNames() ([]string, error)
}

// personalKeywords maps keyword sets to workspace names for personal observations.
// Order matters: first match wins.
var personalKeywords = []struct {
	workspace string
	keywords  []string
}{
	{"finance", []string{"finance", "money", "budget", "salary", "expense", "expenses", "invoice", "tax", "bank", "investment", "savings", "debt", "loan", "mortgage"}},
	{"health", []string{"health", "doctor", "medical", "medicine", "fitness", "workout", "gym", "diet", "nutrition", "therapy", "therapist", "hospital", "symptom", "sick"}},
	{"travel", []string{"travel", "vacation", "trip", "hotel", "flight", "airbnb", "passport", "visa", "holiday", "destination", "itinerary", "booking", "tour"}},
	{"family", []string{"family", "kids", "children", "spouse", "wife", "husband", "partner", "parents", "mom", "dad", "brother", "sister", "wedding", "anniversary"}},
	{"reading-list", []string{"reading", "book", "books", "article", "articles", "reading-list", "readinglist", "read", "novel", "author", "bibliography", "library"}},
}

// personalTopicKeywords are terms that indicate an observation is personal
// regardless of current workspace/workdir.
// These use word-boundary patterns (surrounded by spaces or at start/end)
// to avoid false matches like "personally" or "workspace".
var personalTopicKeywords = []string{
	" personal ", " private ", " my ", " mine ", " myself ",
	"finance", "money", "budget", "salary", "expense",
	"health", "doctor", "medical", "fitness", "workout",
	"family", "kids", "children", "spouse", "wife", "husband",
	"travel", "vacation", "trip", "holiday",
	"reading", "book", "books",
}

// Classifier fills in missing workspace/visibility/organization fields on
// AddObservationParams before they are written to the store.
type Classifier struct {
	defaultWorkspace string
	config           ClassifyConfig
	lister           WorkspaceLister

	mu       sync.Mutex
	orgCache map[string]string // workspace → org, built lazily
}

// NewClassifier creates a Classifier.
// lister is used for runtime organization prefix inference (pass the store).
// defaultWorkspace is the workdir-detected project name (MCPConfig.DefaultProject).
func NewClassifier(lister WorkspaceLister, defaultWorkspace string, cfg ClassifyConfig) *Classifier {
	return &Classifier{
		defaultWorkspace: defaultWorkspace,
		config:           cfg,
		lister:           lister,
		orgCache:         make(map[string]string),
	}
}

// Fill auto-completes missing fields in params following this priority:
//  1. Explicit LLM-provided value → never overridden
//  2. Visibility empty + personal topic keywords in title/content → "personal"
//  3. Visibility empty + defaultWorkspace present → "work"
//  4. Workspace empty + visibility=personal → infer from personal keyword map (default "personal")
//  5. Workspace empty + visibility=work + defaultWorkspace → use defaultWorkspace
//  6. Organization empty + workspace set → infer from DB prefix groups + config overrides
func (c *Classifier) Fill(params *store.AddObservationParams) {
	// Step 1: infer visibility if empty
	if params.Visibility == "" {
		if c.isPersonalTopic(params.Title, params.Content) {
			params.Visibility = "personal"
		} else if c.defaultWorkspace != "" {
			params.Visibility = "work"
		}
	}

	// Step 2: infer workspace if empty
	if params.Workspace == "" {
		if params.Visibility == "personal" {
			params.Workspace = c.inferPersonalWorkspace(params.Title, params.Content)
		} else if params.Visibility == "work" && c.defaultWorkspace != "" {
			params.Workspace = c.defaultWorkspace
		}
	}

	// Step 3: infer organization if empty and workspace is set
	if params.Organization == "" && params.Workspace != "" && params.Visibility != "personal" {
		params.Organization = c.InferOrganization(params.Workspace)
	}
}

// isPersonalTopic returns true if the title or content contains personal topic keywords.
// The haystack is padded with spaces so word-boundary patterns like " my " work
// even at the start or end of the string.
func (c *Classifier) isPersonalTopic(title, content string) bool {
	haystack := " " + strings.ToLower(title+" "+content) + " "
	for _, kw := range personalTopicKeywords {
		if strings.Contains(haystack, kw) {
			return true
		}
	}
	return false
}

// inferPersonalWorkspace returns the workspace name for a personal observation
// based on keyword matching against title+content. Falls back to "personal".
func (c *Classifier) inferPersonalWorkspace(title, content string) string {
	haystack := strings.ToLower(title + " " + content)
	for _, entry := range personalKeywords {
		for _, kw := range entry.keywords {
			if strings.Contains(haystack, kw) {
				return entry.workspace
			}
		}
	}
	return "personal"
}

// InferOrganization returns the organization for a given workspace.
// Priority: explicit config override > cached prefix inference > empty string.
func (c *Classifier) InferOrganization(workspace string) string {
	if workspace == "" {
		return ""
	}

	// Check explicit override map first
	if org, ok := c.config.WorkspaceOrgMap[workspace]; ok {
		return org
	}

	// Check cache
	c.mu.Lock()
	if org, ok := c.orgCache[workspace]; ok {
		c.mu.Unlock()
		return org
	}
	c.mu.Unlock()

	// Build from DB
	org := c.inferOrgFromDB(workspace)

	c.mu.Lock()
	c.orgCache[workspace] = org
	c.mu.Unlock()

	return org
}

// inferOrgFromDB scans existing workspace names to find a common prefix
// shared by 2 or more workspaces that also matches the given workspace.
func (c *Classifier) inferOrgFromDB(workspace string) string {
	if c.lister == nil {
		return ""
	}
	names, err := c.lister.ListProjectNames()
	if err != nil || len(names) < 2 {
		return ""
	}

	// Count how many workspaces share each prefix (min 2 chars, up to first "-")
	// Strategy: find the longest prefix of `workspace` that is shared by ≥2 other workspaces
	prefixCount := make(map[string]int)
	for _, name := range names {
		if name == workspace {
			continue
		}
		// Build prefix candidates: every prefix up to the first "-" separator
		prefix := prefixUpToDash(name)
		if len(prefix) >= 2 {
			prefixCount[prefix]++
		}
	}

	// Find the best (longest) prefix of workspace that has ≥2 matches
	workspacePrefix := prefixUpToDash(workspace)
	if workspacePrefix == "" {
		return ""
	}

	// Check progressively shorter prefixes of workspace
	for i := len(workspacePrefix); i >= 2; i-- {
		candidate := workspacePrefix[:i]
		if prefixCount[candidate] >= 1 { // ≥1 other workspace shares it = ≥2 total
			return candidate
		}
	}
	return ""
}

// prefixUpToDash returns the part of s up to (but not including) the first "-".
// If there is no "-" or the "-" is the first character, returns s unchanged
// (or empty for a leading-dash string, enforced by idx > 0).
func prefixUpToDash(s string) string {
	if idx := strings.Index(s, "-"); idx > 0 {
		return s[:idx]
	}
	if strings.HasPrefix(s, "-") {
		return ""
	}
	return s
}
