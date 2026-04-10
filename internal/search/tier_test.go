package search

import (
	"testing"

	"github.com/Syfra3/ancora/internal/classify"
	"github.com/Syfra3/ancora/internal/store"
)

// ws returns a *string for workspace fields.
func ws(s string) *string { return &s }

// org returns a *string for organization fields.
func org(s string) *string { return &s }

func makeResult(workspace, organization string, rank float64) Result {
	r := Result{
		SearchResult: store.SearchResult{
			Observation: store.Observation{
				Workspace:    ws(workspace),
				Organization: org(organization),
			},
			Rank: rank,
		},
	}
	if workspace == "" {
		r.Workspace = nil
	}
	if organization == "" {
		r.Organization = nil
	}
	return r
}

func balancedCfg() classify.TierConfig { return classify.PresetBalanced.ToTierConfig() }
func strictCfg() classify.TierConfig   { return classify.PresetStrict.ToTierConfig() }
func flatCfg() classify.TierConfig     { return classify.PresetFlat.ToTierConfig() }

// ─── ApplyTierScoring ─────────────────────────────────────────────────────────

func TestApplyTierScoringSameWorkspaceUnchanged(t *testing.T) {
	results := []Result{
		makeResult("glim-api", "glim", 1.0),
	}
	scored := ApplyTierScoring(results, "glim-api", balancedCfg())
	if len(scored) != 1 {
		t.Fatalf("expected 1 result, got %d", len(scored))
	}
	if scored[0].Rank != 1.0 {
		t.Errorf("Tier 1 (same workspace) should be score×1.0, got %v", scored[0].Rank)
	}
}

func TestApplyTierScoringSameOrgDifferentWorkspace(t *testing.T) {
	cfg := balancedCfg()
	results := []Result{
		makeResult("glim-price", "glim", 1.0),
	}
	scored := ApplyTierScoring(results, "glim-api", cfg)
	want := 1.0 * cfg.Tier2Multiplier
	if scored[0].Rank != want {
		t.Errorf("Tier 2 (same org, diff ws): got %v, want %v", scored[0].Rank, want)
	}
}

func TestApplyTierScoringDifferentOrg(t *testing.T) {
	cfg := balancedCfg()
	results := []Result{
		makeResult("other-service", "other", 1.0),
	}
	scored := ApplyTierScoring(results, "glim-api", cfg)
	want := 1.0 * cfg.Tier3Multiplier
	if scored[0].Rank != want {
		t.Errorf("Tier 3 (diff org): got %v, want %v", scored[0].Rank, want)
	}
}

func TestApplyTierScoringEmptyCurrentWorkspaceNoPenalty(t *testing.T) {
	results := []Result{
		makeResult("glim-api", "glim", 0.8),
		makeResult("other-ws", "", 0.9),
	}
	scored := ApplyTierScoring(results, "", balancedCfg())
	// No current workspace → no reranking, original order preserved
	if scored[0].Rank != 0.8 || scored[1].Rank != 0.9 {
		t.Errorf("empty workspace should not change scores: got %v %v", scored[0].Rank, scored[1].Rank)
	}
}

func TestApplyTierScoringRanksCurrentWorkspaceFirst(t *testing.T) {
	cfg := balancedCfg()
	// Other workspace has higher raw score, but current workspace should win after tier
	results := []Result{
		makeResult("other-service", "other", 1.0), // raw score 1.0 but tier 3
		makeResult("glim-api", "glim", 0.7),       // raw score 0.7 but tier 1
	}
	scored := ApplyTierScoring(results, "glim-api", cfg)
	if *scored[0].Workspace != "glim-api" {
		t.Errorf("current workspace should rank first after tier scoring, got %q", *scored[0].Workspace)
	}
}

func TestApplyTierScoringThreeTierOrdering(t *testing.T) {
	cfg := balancedCfg()
	results := []Result{
		makeResult("other-service", "other", 0.9), // Tier 3
		makeResult("glim-price", "glim", 0.85),    // Tier 2 (same org as glim-api)
		makeResult("glim-api", "glim", 0.7),       // Tier 1 (exact match)
	}
	scored := ApplyTierScoring(results, "glim-api", cfg)

	// After tier scoring:
	// glim-api: 0.7 × 1.0 = 0.700
	// glim-price: 0.85 × 0.85 = 0.7225
	// other-service: 0.9 × 0.6 = 0.54
	// Expected order: glim-price > glim-api > other-service
	if *scored[0].Workspace != "glim-price" {
		t.Errorf("expected glim-price first, got %q", derefWorkspace(scored[0].Workspace))
	}
	if *scored[1].Workspace != "glim-api" {
		t.Errorf("expected glim-api second, got %q", derefWorkspace(scored[1].Workspace))
	}
	if *scored[2].Workspace != "other-service" {
		t.Errorf("expected other-service third, got %q", derefWorkspace(scored[2].Workspace))
	}
}

func TestApplyTierScoringStrictPreset(t *testing.T) {
	cfg := strictCfg()
	results := []Result{
		makeResult("glim-price", "glim", 1.0), // Tier 2
	}
	scored := ApplyTierScoring(results, "glim-api", cfg)
	want := 1.0 * cfg.Tier2Multiplier // 0.60
	if scored[0].Rank != want {
		t.Errorf("strict Tier 2: got %v, want %v", scored[0].Rank, want)
	}
}

func TestApplyTierScoringFlatPresetNoPenalty(t *testing.T) {
	results := []Result{
		makeResult("other-service", "other", 0.5),
	}
	scored := ApplyTierScoring(results, "glim-api", flatCfg())
	if scored[0].Rank != 0.5 {
		t.Errorf("flat preset should not change scores, got %v", scored[0].Rank)
	}
}

func TestApplyTierScoringZeroConfigFallsBackToBalanced(t *testing.T) {
	zero := classify.TierConfig{} // both multipliers == 0
	results := []Result{
		makeResult("glim-price", "glim", 1.0),
	}
	scored := ApplyTierScoring(results, "glim-api", zero)
	// Should behave as balanced (Tier2=0.85)
	want := classify.PresetBalanced.ToTierConfig().Tier2Multiplier
	if scored[0].Rank != want {
		t.Errorf("zero config should fall back to balanced T2=%v, got %v", want, scored[0].Rank)
	}
}

func TestApplyTierScoringInfersOrgFromWorkspacePrefix(t *testing.T) {
	cfg := balancedCfg()
	// Result has no stored organization — should infer "glim" from "glim-price" prefix
	r := makeResult("glim-price", "", 1.0)
	r.Organization = nil
	results := []Result{r}

	scored := ApplyTierScoring(results, "glim-api", cfg)
	// glim-price inferred org=glim, current workspace org=glim → Tier 2
	want := 1.0 * cfg.Tier2Multiplier
	if scored[0].Rank != want {
		t.Errorf("org inferred from prefix: expected T2 score %v, got %v", want, scored[0].Rank)
	}
}

func TestApplyTierScoringEmptyResults(t *testing.T) {
	scored := ApplyTierScoring(nil, "glim-api", balancedCfg())
	if scored != nil {
		t.Errorf("nil input should return nil, got %v", scored)
	}
}

// ─── orgFromWorkspace helper ──────────────────────────────────────────────────

func TestOrgFromWorkspace(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"glim-api", "glim"},
		{"glim-price-v2", "glim"},
		{"syfra-cloud", "syfra"},
		{"ancora", ""}, // no dash → no org
		{"", ""},
	}
	for _, tt := range tests {
		got := orgFromWorkspace(tt.input)
		if got != tt.want {
			t.Errorf("orgFromWorkspace(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ─── applyRecencyBoost ────────────────────────────────────────────────────────

func TestApplyRecencyBoostOrdersByRecency(t *testing.T) {
	results := []Result{
		makeResult("old-ws", "", 0.5),
		makeResult("recent-ws", "", 0.5),
	}
	recent := []string{"recent-ws", "old-ws"}
	boosted := applyRecencyBoost(results, recent)

	if *boosted[0].Workspace != "recent-ws" {
		t.Errorf("most recent workspace should rank first, got %q", *boosted[0].Workspace)
	}
}

func TestApplyRecencyBoostEmptyRecentList(t *testing.T) {
	results := []Result{
		makeResult("ws-a", "", 0.9),
		makeResult("ws-b", "", 0.5),
	}
	boosted := applyRecencyBoost(results, nil)
	// No change — original order maintained
	if *boosted[0].Workspace != "ws-a" {
		t.Errorf("empty recency list should preserve original order")
	}
}

func TestApplyRecencyBoostNoChangeForUnknownWorkspace(t *testing.T) {
	results := []Result{
		makeResult("unknown-ws", "", 0.5),
	}
	boosted := applyRecencyBoost(results, []string{"other-ws"})
	if boosted[0].Rank != 0.5 {
		t.Errorf("unknown workspace should not be boosted, got %v", boosted[0].Rank)
	}
}
