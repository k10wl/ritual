package domain

import (
	"os"
	"path/filepath"
	"testing"

	"ritual/internal/config"
)

func TestDefaultSettings(t *testing.T) {
	settings := DefaultSettings()

	if settings.IP != "0.0.0.0" {
		t.Errorf("expected IP 0.0.0.0, got %s", settings.IP)
	}
	if settings.Port != 25565 {
		t.Errorf("expected Port 25565, got %d", settings.Port)
	}
	if settings.Memory != 4096 {
		t.Errorf("expected Memory 4096, got %d", settings.Memory)
	}
}

func TestSettingsValidate(t *testing.T) {
	tests := []struct {
		name    string
		settings *Settings
		wantErr bool
	}{
		{
			name:     "valid settings",
			settings: &Settings{IP: "0.0.0.0", Port: 25565, Memory: 4096},
			wantErr:  false,
		},
		{
			name:     "empty IP",
			settings: &Settings{IP: "", Port: 25565, Memory: 4096},
			wantErr:  true,
		},
		{
			name:     "zero port",
			settings: &Settings{IP: "0.0.0.0", Port: 0, Memory: 4096},
			wantErr:  true,
		},
		{
			name:     "negative port",
			settings: &Settings{IP: "0.0.0.0", Port: -1, Memory: 4096},
			wantErr:  true,
		},
		{
			name:     "port too high",
			settings: &Settings{IP: "0.0.0.0", Port: 65536, Memory: 4096},
			wantErr:  true,
		},
		{
			name:     "zero memory",
			settings: &Settings{IP: "0.0.0.0", Port: 25565, Memory: 0},
			wantErr:  true,
		},
		{
			name:     "negative memory",
			settings: &Settings{IP: "0.0.0.0", Port: 25565, Memory: -1},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSettingsToServer(t *testing.T) {
	settings := &Settings{IP: "192.168.1.1", Port: 25566, Memory: 8192}

	server, err := settings.ToServer()
	if err != nil {
		t.Fatalf("ToServer() error = %v", err)
	}

	if server.IP != "192.168.1.1" {
		t.Errorf("expected IP 192.168.1.1, got %s", server.IP)
	}
	if server.Port != 25566 {
		t.Errorf("expected Port 25566, got %d", server.Port)
	}
	if server.Memory != 8192 {
		t.Errorf("expected Memory 8192, got %d", server.Memory)
	}
}

func TestSettingsSaveAndLoad(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()
	originalRootPath := config.RootPath
	config.RootPath = tempDir
	defer func() { config.RootPath = originalRootPath }()

	// Save settings
	settings := &Settings{IP: "10.0.0.1", Port: 25570, Memory: 2048}
	err := settings.Save()
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	settingsPath := filepath.Join(tempDir, SettingsFilename)
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Fatal("settings file was not created")
	}

	// Load settings
	loaded, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}

	if loaded.IP != settings.IP {
		t.Errorf("expected IP %s, got %s", settings.IP, loaded.IP)
	}
	if loaded.Port != settings.Port {
		t.Errorf("expected Port %d, got %d", settings.Port, loaded.Port)
	}
	if loaded.Memory != settings.Memory {
		t.Errorf("expected Memory %d, got %d", settings.Memory, loaded.Memory)
	}
}

func TestLoadSettingsReturnsDefaultWhenFileNotExists(t *testing.T) {
	// Create temp directory with no settings file
	tempDir := t.TempDir()
	originalRootPath := config.RootPath
	config.RootPath = tempDir
	defer func() { config.RootPath = originalRootPath }()

	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}

	defaults := DefaultSettings()
	if settings.IP != defaults.IP || settings.Port != defaults.Port || settings.Memory != defaults.Memory {
		t.Errorf("expected default settings, got %+v", settings)
	}
}

func TestSettingsSavePrettyPrints(t *testing.T) {
	tempDir := t.TempDir()
	originalRootPath := config.RootPath
	config.RootPath = tempDir
	defer func() { config.RootPath = originalRootPath }()

	settings := &Settings{IP: "0.0.0.0", Port: 25565, Memory: 4096}
	err := settings.Save()
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Read raw file content
	content, err := os.ReadFile(filepath.Join(tempDir, SettingsFilename))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Check for indentation (pretty print uses 2 spaces, same as manifest)
	expected := `{
  "ip": "0.0.0.0",
  "port": 25565,
  "memory": 4096
}`
	if string(content) != expected {
		t.Errorf("expected pretty printed JSON:\n%s\n\ngot:\n%s", expected, string(content))
	}
}
