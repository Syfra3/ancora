package classify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTierConfigPresets(t *testing.T) {
	tests := []struct {
		preset  TierPreset
		wantT2  float64
		wantT3  float64
		wantStr string
	}{
		{PresetBalanced, 0.85, 0.60, "Balanced"},
		{PresetStrict, 0.60, 0.30, "Strict"},
		{PresetFlat, 1.00, 1.00, "Flat"},
	}
	for _, tt := range tests {
		cfg := tt.preset.ToTierConfig()
		if cfg.Tier2Multiplier != tt.wantT2 {
			t.Errorf("preset %s: T2=%v want %v", tt.preset, cfg.Tier2Multiplier, tt.wantT2)
		}
		if cfg.Tier3Multiplier != tt.wantT3 {
			t.Errorf("preset %s: T3=%v want %v", tt.preset, cfg.Tier3Multiplier, tt.wantT3)
		}
		if tt.preset.String() != tt.wantStr {
			t.Errorf("preset String()=%q want %q", tt.preset.String(), tt.wantStr)
		}
	}
}

func TestToTierConfigUnknownPresetFallsBackToBalanced(t *testing.T) {
	unknown := TierPreset(99)
	cfg := unknown.ToTierConfig()
	balanced := PresetBalanced.ToTierConfig()
	if cfg != balanced {
		t.Errorf("unknown preset should fall back to balanced, got %+v", cfg)
	}
}

func TestLoadSaveClassifyConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "classify.json")

	original := ClassifyConfig{
		Preset:          PresetStrict,
		WorkspaceOrgMap: map[string]string{"ancora": "syfra", "glim-api": "glim"},
	}

	if err := SaveClassifyConfig(path, original); err != nil {
		t.Fatalf("SaveClassifyConfig: %v", err)
	}

	loaded := LoadClassifyConfig(path)
	if loaded.Preset != original.Preset {
		t.Errorf("Preset mismatch: got %v want %v", loaded.Preset, original.Preset)
	}
	for k, v := range original.WorkspaceOrgMap {
		if loaded.WorkspaceOrgMap[k] != v {
			t.Errorf("WorkspaceOrgMap[%q]: got %q want %q", k, loaded.WorkspaceOrgMap[k], v)
		}
	}
}

func TestLoadClassifyConfigMissingFileReturnsDefaults(t *testing.T) {
	cfg := LoadClassifyConfig("/nonexistent/path/classify.json")
	def := DefaultClassifyConfig()
	if cfg.Preset != def.Preset {
		t.Errorf("missing file: Preset=%v want %v", cfg.Preset, def.Preset)
	}
	if cfg.WorkspaceOrgMap == nil {
		t.Error("missing file: WorkspaceOrgMap should not be nil")
	}
}

func TestLoadClassifyConfigCorruptJSONReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "classify.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	cfg := LoadClassifyConfig(path)
	def := DefaultClassifyConfig()
	if cfg.Preset != def.Preset {
		t.Errorf("corrupt json: Preset=%v want %v", cfg.Preset, def.Preset)
	}
}

func TestSaveClassifyConfigCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "classify.json")
	if err := SaveClassifyConfig(path, DefaultClassifyConfig()); err != nil {
		t.Fatalf("SaveClassifyConfig with nested dirs: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestLoadClassifyConfigNilMapBecomesEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "classify.json")
	// Write JSON with null map
	if err := os.WriteFile(path, []byte(`{"preset":0}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := LoadClassifyConfig(path)
	if cfg.WorkspaceOrgMap == nil {
		t.Error("WorkspaceOrgMap should be non-nil even when JSON omits it")
	}
}
