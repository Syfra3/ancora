package search

import (
	"sort"
	"strings"

	"github.com/Syfra3/ancora/internal/classify"
)

// ApplyTierScoring re-ranks search results based on workspace proximity to
// the current session workspace. Results from the same workspace rank highest,
// followed by the same organization, then everything else.
//
// Tier 1 — same workspace:         score × 1.0 (no change)
// Tier 2 — same org, diff ws:      score × cfg.Tier2Multiplier
// Tier 3 — different org / no org: score × cfg.Tier3Multiplier
//
// If currentWorkspace is empty (fresh chat with no detected project),
// no penalty is applied — all results are ranked by pure relevance.
// If cfg is the zero value (both multipliers == 0), balanced defaults are used.
func ApplyTierScoring(results []Result, currentWorkspace string, cfg classify.TierConfig) []Result {
	if len(results) == 0 {
		return results
	}
	// No current workspace → no penalty (fresh chat)
	if strings.TrimSpace(currentWorkspace) == "" {
		return results
	}
	// Zero-value config → use balanced defaults
	if cfg.Tier2Multiplier == 0 && cfg.Tier3Multiplier == 0 {
		cfg = classify.PresetBalanced.ToTierConfig()
	}

	currentOrg := orgFromWorkspace(currentWorkspace)

	scored := make([]Result, len(results))
	for i, r := range results {
		ws := ""
		if r.Workspace != nil {
			ws = *r.Workspace
		}
		org := ""
		if r.Organization != nil {
			org = *r.Organization
		}
		// Infer org from workspace prefix if not stored
		if org == "" && ws != "" {
			org = orgFromWorkspace(ws)
		}

		multiplier := cfg.Tier3Multiplier
		switch {
		case ws == currentWorkspace:
			multiplier = 1.0
		case org != "" && org == currentOrg:
			multiplier = cfg.Tier2Multiplier
		}

		adjusted := r
		// Apply tier penalty correctly for both positive (RRF/semantic) and
		// negative (FTS5 BM25) rank values.
		// - Positive ranks: multiply reduces the score (worse rank)
		// - Negative ranks: divide makes them more negative (worse rank)
		// Both operations move the adjusted score away from the "best" end.
		if r.Rank >= 0 {
			adjusted.Rank = r.Rank * multiplier
		} else {
			adjusted.Rank = r.Rank / multiplier
		}
		scored[i] = adjusted
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Rank > scored[j].Rank
	})
	return scored
}

// orgFromWorkspace infers an organization from a workspace name using the
// same prefix-up-to-dash logic as the classify package.
// e.g. "glim-api" → "glim", "ancora" → "" (solo, no prefix group)
func orgFromWorkspace(workspace string) string {
	if idx := strings.Index(workspace, "-"); idx > 0 {
		return workspace[:idx]
	}
	return ""
}

// applyRecencyBoost applies a mild score boost to recently-used workspaces
// when no current workspace is known (fresh chat). The boost is proportional
// to position in the recency list (index 0 = most recent = highest boost).
// This is a no-op if recentWorkspaces is empty.
func applyRecencyBoost(results []Result, recentWorkspaces []string) []Result {
	if len(results) == 0 || len(recentWorkspaces) == 0 {
		return results
	}

	// Build recency rank map: most recent = highest rank
	rankMap := make(map[string]float64, len(recentWorkspaces))
	for i, ws := range recentWorkspaces {
		// boost decays from 0.15 (most recent) to ~0.02 (oldest in list)
		boost := 0.15 * (1.0 - float64(i)/float64(len(recentWorkspaces)))
		rankMap[ws] = boost
	}

	boosted := make([]Result, len(results))
	for i, r := range results {
		ws := ""
		if r.Workspace != nil {
			ws = *r.Workspace
		}
		adjusted := r
		if boost, ok := rankMap[ws]; ok {
			adjusted.Rank = r.Rank + boost
		}
		boosted[i] = adjusted
	}

	sort.SliceStable(boosted, func(i, j int) bool {
		return boosted[i].Rank > boosted[j].Rank
	})
	return boosted
}

// derefWorkspace safely dereferences a *string workspace field.
func derefWorkspace(ws *string) string {
	if ws == nil {
		return ""
	}
	return *ws
}
