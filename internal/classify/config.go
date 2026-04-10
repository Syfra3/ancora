// Package classify provides workspace/visibility/organization auto-classification
// for ancora observations, and tier-based search scoring configuration.
package classify

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// TierPreset controls how aggressively search results are penalized
// when they come from a different workspace or organization than the current one.
type TierPreset int

const (
	// PresetBalanced is the default — moderate boost for same-workspace results.
	PresetBalanced TierPreset = iota
	// PresetStrict applies a heavy penalty to cross-workspace and cross-org results.
	PresetStrict
	// PresetFlat disables tier scoring entirely — pure relevance ranking.
	PresetFlat
)

// TierConfig holds the score multipliers for Tier 2 (same org, different workspace)
// and Tier 3 (different org / no org match).
// Tier 1 (same workspace) always uses multiplier 1.0.
type TierConfig struct {
	Tier2Multiplier float64
	Tier3Multiplier float64
}

// presetConfigs maps each TierPreset to its TierConfig multipliers.
var presetConfigs = map[TierPreset]TierConfig{
	PresetBalanced: {Tier2Multiplier: 0.85, Tier3Multiplier: 0.60},
	PresetStrict:   {Tier2Multiplier: 0.60, Tier3Multiplier: 0.30},
	PresetFlat:     {Tier2Multiplier: 1.00, Tier3Multiplier: 1.00},
}

// ToTierConfig converts the preset to its TierConfig multipliers.
// Unknown presets fall back to Balanced.
func (p TierPreset) ToTierConfig() TierConfig {
	if cfg, ok := presetConfigs[p]; ok {
		return cfg
	}
	return presetConfigs[PresetBalanced]
}

// String returns the human-readable name of the preset.
func (p TierPreset) String() string {
	switch p {
	case PresetStrict:
		return "Strict"
	case PresetFlat:
		return "Flat"
	default:
		return "Balanced"
	}
}

// ClassifyConfig is the top-level configuration for auto-classification and
// tier scoring. It is stored at ~/.ancora/classify.json.
type ClassifyConfig struct {
	// Preset controls the tier scoring multipliers.
	// Default: PresetBalanced.
	Preset TierPreset `json:"preset"`

	// WorkspaceOrgMap is an explicit workspace→organization override map.
	// e.g. {"ancora": "syfra"} forces workspace "ancora" to org "syfra"
	// regardless of prefix inference.
	WorkspaceOrgMap map[string]string `json:"workspace_org_map,omitempty"`
}

// DefaultClassifyConfig returns a ClassifyConfig with sensible defaults.
func DefaultClassifyConfig() ClassifyConfig {
	return ClassifyConfig{
		Preset:          PresetBalanced,
		WorkspaceOrgMap: map[string]string{},
	}
}

// DefaultConfigPath returns the default path for classify.json:
// ~/.ancora/classify.json
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "classify.json"
	}
	return filepath.Join(home, ".ancora", "classify.json")
}

// LoadClassifyConfig reads the config from path.
// If the file does not exist or cannot be parsed, returns DefaultClassifyConfig.
func LoadClassifyConfig(path string) ClassifyConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultClassifyConfig()
	}
	var cfg ClassifyConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultClassifyConfig()
	}
	// Ensure the map is never nil
	if cfg.WorkspaceOrgMap == nil {
		cfg.WorkspaceOrgMap = map[string]string{}
	}
	return cfg
}

// SaveClassifyConfig writes cfg to path, creating parent dirs as needed.
func SaveClassifyConfig(path string, cfg ClassifyConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
