package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"ritual/internal/config"
)

// SettingsFilename is the on-disk filename for the user settings file.
const SettingsFilename = "settings.json"

// Settings represents user-configurable server settings
type Settings struct {
	Port           int            `json:"port"`
	Memory         int            `json:"memory"`
	LocalRetention RetentionRules `json:"local_retention"`
}

// DefaultSettings returns default settings values
func DefaultSettings() *Settings {
	return &Settings{
		Port:           25565,
		Memory:         4096,
		LocalRetention: DefaultRetentionRules(),
	}
}

// SettingsPath returns the full path to the settings file
func SettingsPath() string {
	return filepath.Join(config.RootPath, SettingsFilename)
}

// LoadSettings loads settings from the settings file
// Returns default settings if file doesn't exist
func LoadSettings() (*Settings, error) {
	path := SettingsPath()

	data, err := os.ReadFile(path) // #nosec G304 -- project-scoped config path
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultSettings(), nil
		}
		return nil, fmt.Errorf("failed to read settings file: %w", err)
	}

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse settings file: %w", err)
	}

	return &settings, nil
}

// Save saves settings to the settings file with pretty formatting
func (s *Settings) Save() error {
	path := SettingsPath()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := os.WriteFile(path, data, config.FilePermission); err != nil {
		return fmt.Errorf("failed to write settings file: %w", err)
	}

	return nil
}

// ToServerRuntime creates a ServerRuntime instance from settings
func (s *Settings) ToServerRuntime() (*ServerRuntime, error) {
	return NewServerRuntime(s.Port, s.Memory)
}

// Validate checks if settings values are valid
func (s *Settings) Validate() error {
	if s.Port <= 0 || s.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if s.Memory <= 0 {
		return errors.New("memory must be positive")
	}
	return nil
}
