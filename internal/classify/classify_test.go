package classify

import (
	"testing"

	"github.com/Syfra3/ancora/internal/store"
)

// ─── Mock WorkspaceLister ─────────────────────────────────────────────────────

type mockLister struct {
	names []string
	err   error
}

func (m *mockLister) ListProjectNames() ([]string, error) {
	return m.names, m.err
}

func newClassifier(t *testing.T, defaultWS string, names []string, overrides map[string]string) *Classifier {
	t.Helper()
	cfg := DefaultClassifyConfig()
	if overrides != nil {
		cfg.WorkspaceOrgMap = overrides
	}
	return NewClassifier(&mockLister{names: names}, defaultWS, cfg)
}

// ─── Fill: Visibility inference ───────────────────────────────────────────────

func TestFillVisibilityPersonalKeyword(t *testing.T) {
	c := newClassifier(t, "glim-api", nil, nil)

	tests := []struct {
		title   string
		content string
	}{
		{"My budget for 2026", "expenses and savings"},
		{"Doctor appointment", ""},
		{"Family trip", "planning vacation"},
		{"reading list", "books to read this year"},
		{"", "personal note about myself"},
	}

	for _, tt := range tests {
		params := store.AddObservationParams{Title: tt.title, Content: tt.content}
		c.Fill(&params)
		if params.Visibility != "personal" {
			t.Errorf("title=%q content=%q: expected visibility=personal, got %q", tt.title, tt.content, params.Visibility)
		}
	}
}

func TestFillVisibilityWorkWhenDefaultProjectSet(t *testing.T) {
	c := newClassifier(t, "glim-api", nil, nil)
	params := store.AddObservationParams{
		Title:   "Refactor auth middleware",
		Content: "Use JWT tokens for stateless auth",
	}
	c.Fill(&params)
	if params.Visibility != "work" {
		t.Errorf("expected visibility=work, got %q", params.Visibility)
	}
}

func TestFillVisibilityEmptyWhenNoDefaultProject(t *testing.T) {
	c := newClassifier(t, "", nil, nil)
	params := store.AddObservationParams{
		Title:   "Some neutral observation",
		Content: "Nothing personal, nothing work-specific",
	}
	c.Fill(&params)
	// No workdir, no personal keywords → visibility stays empty (unknown)
	if params.Visibility != "" {
		t.Errorf("expected visibility empty when no workdir and no keywords, got %q", params.Visibility)
	}
}

func TestFillExplicitVisibilityNeverOverridden(t *testing.T) {
	c := newClassifier(t, "glim-api", nil, nil)
	params := store.AddObservationParams{
		Title:      "family vacation",
		Content:    "budget for trip",
		Visibility: "work", // explicitly set — should NOT be overridden
	}
	c.Fill(&params)
	if params.Visibility != "work" {
		t.Errorf("explicit visibility=work should not be overridden, got %q", params.Visibility)
	}
}

// ─── Fill: Workspace inference ────────────────────────────────────────────────

func TestFillWorkspacePersonalKeywordMap(t *testing.T) {
	c := newClassifier(t, "glim-api", nil, nil)

	tests := []struct {
		title         string
		content       string
		wantWorkspace string
	}{
		{"My budget this month", "salary and expenses", "finance"},
		{"Doctor appointment", "medical checkup", "health"},
		{"Family vacation plans", "trip to spain", "travel"},
		{"Books to read", "reading list for 2026", "reading-list"},
		{"Wife's birthday", "planning anniversary dinner", "family"},
		{"Personal diary", "just a note", "personal"},
	}

	for _, tt := range tests {
		params := store.AddObservationParams{Title: tt.title, Content: tt.content}
		c.Fill(&params)
		if params.Workspace != tt.wantWorkspace {
			t.Errorf("title=%q: expected workspace=%q, got %q", tt.title, tt.wantWorkspace, params.Workspace)
		}
		if params.Visibility != "personal" {
			t.Errorf("title=%q: expected visibility=personal, got %q", tt.title, params.Visibility)
		}
	}
}

func TestFillWorkspaceUsesDefaultProjectForWork(t *testing.T) {
	c := newClassifier(t, "glim-api", nil, nil)
	params := store.AddObservationParams{
		Title:   "Add rate limiting to API",
		Content: "Implement token bucket algorithm",
	}
	c.Fill(&params)
	if params.Workspace != "glim-api" {
		t.Errorf("expected workspace=glim-api, got %q", params.Workspace)
	}
}

func TestFillWorkspaceExplicitNeverOverridden(t *testing.T) {
	c := newClassifier(t, "glim-api", nil, nil)
	params := store.AddObservationParams{
		Title:     "Fix bug",
		Content:   "patch in glim-price",
		Workspace: "glim-price", // explicit
	}
	c.Fill(&params)
	if params.Workspace != "glim-price" {
		t.Errorf("explicit workspace should not be overridden, got %q", params.Workspace)
	}
}

// ─── InferOrganization ────────────────────────────────────────────────────────

func TestInferOrganizationPrefixMatch(t *testing.T) {
	c := newClassifier(t, "", []string{"glim-api", "glim-price", "glim-auth", "ancora"}, nil)

	org := c.InferOrganization("glim-api")
	if org != "glim" {
		t.Errorf("expected org=glim, got %q", org)
	}

	// ancora has no other ancora-* workspaces → no org
	org = c.InferOrganization("ancora")
	if org != "" {
		t.Errorf("expected empty org for solo workspace, got %q", org)
	}
}

func TestInferOrganizationExplicitOverrideTakesPriority(t *testing.T) {
	overrides := map[string]string{"ancora": "syfra"}
	c := newClassifier(t, "", []string{"ancora", "glim-api"}, overrides)

	org := c.InferOrganization("ancora")
	if org != "syfra" {
		t.Errorf("expected org=syfra from override, got %q", org)
	}
}

func TestInferOrganizationCachesResult(t *testing.T) {
	calls := 0
	lister := &countingLister{
		names:  []string{"glim-api", "glim-price"},
		onCall: func() { calls++ },
	}
	cfg := DefaultClassifyConfig()
	c := NewClassifier(lister, "", cfg)

	c.InferOrganization("glim-api")
	c.InferOrganization("glim-api") // second call should use cache
	c.InferOrganization("glim-api") // third call should use cache

	if calls > 1 {
		t.Errorf("expected DB called once (cached), got %d calls", calls)
	}
}

func TestInferOrganizationEmptyWorkspaceReturnsEmpty(t *testing.T) {
	c := newClassifier(t, "", []string{"glim-api", "glim-price"}, nil)
	if org := c.InferOrganization(""); org != "" {
		t.Errorf("empty workspace should return empty org, got %q", org)
	}
}

func TestInferOrganizationNilListerReturnsEmpty(t *testing.T) {
	cfg := DefaultClassifyConfig()
	c := NewClassifier(nil, "", cfg)
	if org := c.InferOrganization("glim-api"); org != "" {
		t.Errorf("nil lister should return empty org, got %q", org)
	}
}

// ─── prefixUpToDash helper ────────────────────────────────────────────────────

func TestPrefixUpToDash(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"glim-api", "glim"},
		{"glim-price-v2", "glim"},
		{"ancora", "ancora"},
		{"syfra-cloud", "syfra"},
		{"", ""},
		{"-leading", ""},
	}
	for _, tt := range tests {
		got := prefixUpToDash(tt.input)
		if got != tt.want {
			t.Errorf("prefixUpToDash(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ─── Fill: Organization inferred via Fill ────────────────────────────────────

func TestFillInfersOrganizationFromPrefix(t *testing.T) {
	c := newClassifier(t, "glim-api", []string{"glim-api", "glim-price", "glim-auth"}, nil)
	params := store.AddObservationParams{
		Title:   "JWT auth refactor",
		Content: "work refactoring",
	}
	c.Fill(&params)
	if params.Organization != "glim" {
		t.Errorf("expected organization=glim, got %q", params.Organization)
	}
}

func TestFillPersonalObservationHasNoOrganization(t *testing.T) {
	c := newClassifier(t, "glim-api", []string{"glim-api", "glim-price"}, nil)
	params := store.AddObservationParams{
		Title:   "My budget this month",
		Content: "salary and expenses",
	}
	c.Fill(&params)
	if params.Organization != "" {
		t.Errorf("personal obs should have no organization, got %q", params.Organization)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

type countingLister struct {
	names  []string
	onCall func()
}

func (c *countingLister) ListProjectNames() ([]string, error) {
	c.onCall()
	return c.names, nil
}
