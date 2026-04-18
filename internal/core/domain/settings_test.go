package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"ritual/internal/config"
	"testing"
)

func TestDefaultSettings(t *testing.T) {
	settings := DefaultSettings()

	if settings.Port != 25565 {
		t.Errorf("expected Port 25565, got %d", settings.Port)
	}
	if settings.Memory != 4096 {
		t.Errorf("expected Memory 4096, got %d", settings.Memory)
	}
}

func TestSettingsValidate(t *testing.T) {
	tests := []struct {
		name     string
		settings *Settings
		wantErr  bool
	}{
		{
			name:     "valid settings",
			settings: &Settings{Port: 25565, Memory: 4096},
			wantErr:  false,
		},
		{
			name:     "zero port",
			settings: &Settings{Port: 0, Memory: 4096},
			wantErr:  true,
		},
		{
			name:     "negative port",
			settings: &Settings{Port: -1, Memory: 4096},
			wantErr:  true,
		},
		{
			name:     "port too high",
			settings: &Settings{Port: 65536, Memory: 4096},
			wantErr:  true,
		},
		{
			name:     "zero memory",
			settings: &Settings{Port: 25565, Memory: 0},
			wantErr:  true,
		},
		{
			name:     "negative memory",
			settings: &Settings{Port: 25565, Memory: -1},
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

func TestSettingsToServerRuntime(t *testing.T) {
	settings := &Settings{Port: 25566, Memory: 8192}

	server, err := settings.ToServerRuntime()
	if err != nil {
		t.Fatalf("ToServer() error = %v", err)
	}

	if server.Port != 25566 {
		t.Errorf("expected Port 25566, got %d", server.Port)
	}
	if server.Memory != 8192 {
		t.Errorf("expected Memory 8192, got %d", server.Memory)
	}
}

func TestSettingsSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	originalRootPath := config.RootPath
	config.RootPath = tempDir
	defer func() { config.RootPath = originalRootPath }()

	settings := &Settings{Port: 25570, Memory: 2048}
	err := settings.Save()
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	settingsPath := filepath.Join(tempDir, SettingsFilename)
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Fatal("settings file was not created")
	}

	loaded, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}

	if loaded.Port != settings.Port {
		t.Errorf("expected Port %d, got %d", settings.Port, loaded.Port)
	}
	if loaded.Memory != settings.Memory {
		t.Errorf("expected Memory %d, got %d", settings.Memory, loaded.Memory)
	}
}

func TestLoadSettingsReturnsDefaultWhenFileNotExists(t *testing.T) {
	tempDir := t.TempDir()
	originalRootPath := config.RootPath
	config.RootPath = tempDir
	defer func() { config.RootPath = originalRootPath }()

	settings, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings() error = %v", err)
	}

	defaults := DefaultSettings()
	if settings.Port != defaults.Port || settings.Memory != defaults.Memory {
		t.Errorf("expected default settings, got %+v", settings)
	}
}

func TestSettingsSavePrettyPrints(t *testing.T) {
	tempDir := t.TempDir()
	originalRootPath := config.RootPath
	config.RootPath = tempDir
	defer func() { config.RootPath = originalRootPath }()

	settings := &Settings{Port: 25565, Memory: 4096}
	err := settings.Save()
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tempDir, SettingsFilename))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	expected := `{
  "port": 25565,
  "memory": 4096,
  "local_retention": {
    "keep_last": 0,
    "keep_daily": 0,
    "keep_weekly": 0,
    "keep_monthly": 0
  }
}`
	if string(content) != expected {
		t.Errorf("expected pretty printed JSON:\n%s\n\ngot:\n%s", expected, string(content))
	}
}

func TestLoadSettings_MissingRetention_UsesZeroValue(t *testing.T) {
	data := []byte(`{"port":25565,"memory":8192}`)

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("should load v1 settings: %v", err)
	}
	if s.Port != 25565 {
		t.Errorf("Port=%d, want 25565", s.Port)
	}
	if s.LocalRetention != (RetentionRules{}) {
		t.Errorf("LocalRetention = %+v, want zero (missing field)", s.LocalRetention)
	}
}
