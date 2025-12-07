package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ritual/internal/config"
)

const SettingsFilename = "settings.json"

// Settings represents user-configurable server settings
type Settings struct {
	IP     string `json:"ip"`
	Port   int    `json:"port"`
	Memory int    `json:"memory"`
}

// DefaultSettings returns default settings values
func DefaultSettings() *Settings {
	return &Settings{
		IP:     "0.0.0.0",
		Port:   25565,
		Memory: 4096,
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

	data, err := os.ReadFile(path)
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

// ToServer creates a Server instance from settings
func (s *Settings) ToServer() (*Server, error) {
	address := fmt.Sprintf("%s:%d", s.IP, s.Port)
	return NewServer(address, s.Memory)
}

// Validate checks if settings values are valid
func (s *Settings) Validate() error {
	if s.IP == "" {
		return fmt.Errorf("IP cannot be empty")
	}
	if s.Port <= 0 || s.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if s.Memory <= 0 {
		return fmt.Errorf("memory must be positive")
	}
	return nil
}
