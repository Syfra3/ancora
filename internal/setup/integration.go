package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type IntegrationState struct {
	Mode      string `json:"mode"`
	MCPTarget string `json:"mcp_target,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func integrationStatePath() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".syfra", "integration.json"), nil
}

func LoadIntegrationState() (*IntegrationState, error) {
	path, err := integrationStatePath()
	if err != nil {
		return nil, err
	}
	data, err := readFileFn(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read integration state: %w", err)
	}
	var state IntegrationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse integration state: %w", err)
	}
	if state.Mode != ModeAncoraOnly && state.Mode != ModeAncoraVela && state.Mode != "vela-only" {
		return nil, nil
	}
	return &state, nil
}

func SaveIntegrationState(state IntegrationState) error {
	if state.Mode == "" {
		return fmt.Errorf("integration mode is required")
	}
	if state.UpdatedAt == "" {
		state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if state.UpdatedBy == "" {
		state.UpdatedBy = "ancora"
	}
	path, err := integrationStatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create integration dir: %w", err)
	}
	data, err := jsonMarshalIndentFn(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal integration state: %w", err)
	}
	if err := writeFileFn(path, data, 0o644); err != nil {
		return fmt.Errorf("write integration state: %w", err)
	}
	return nil
}
