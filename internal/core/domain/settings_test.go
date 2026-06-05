package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"ritual/internal/config"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSettings(t *testing.T) {
	settings := DefaultSettings()

	assert.Equal(t, 25565, settings.Port, "default Port must match documented value")
	assert.Equal(t, 4096, settings.Memory, "default Memory must match documented value")
	assert.Equal(t, config.DefaultMinRAMMB, settings.MinRAMMB, "default MinRAMMB must come from config defaults so manifest fallback parity holds")
	assert.Equal(t, config.DefaultMinDiskMB, settings.MinDiskMB, "default MinDiskMB must come from config defaults so manifest fallback parity holds")
	assert.Equal(t, config.DefaultMinJavaVersion, settings.MinJavaVersion, "default MinJavaVersion must come from config defaults so manifest fallback parity holds")
	assert.Equal(t, "start.bat", settings.StartScript, "default StartScript must be 'start.bat' — NeoForge ships start.bat as the canonical Windows launcher and operators expect that filename without configuring anything")
}

func TestLoadSettings_BackfillsEmptyStartScript_ToStartBat(t *testing.T) {
	tempDir := t.TempDir()
	originalRootPath := config.RootPath
	config.RootPath = tempDir
	defer func() { config.RootPath = originalRootPath }()

	pre := []byte(`{"port":25570,"memory":8192,"start_script":""}`)
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, SettingsFilename), pre, 0o644),
		"seed a settings.json with an empty start_script so the loader's backfill is exercised")

	loaded, err := LoadSettings()
	require.NoError(t, err, "LoadSettings must succeed against a settings.json with an empty start_script — empty is treated as 'use default', not a hard error")
	assert.Equal(t, "start.bat", loaded.StartScript, "an empty start_script on disk must be backfilled to 'start.bat' so a v2.0 settings.json (no field) and a deliberately-cleared field both produce a runnable launcher path")
}

func validSettings() *Settings {
	return &Settings{
		Port:           25565,
		Memory:         4096,
		MinRAMMB:       config.DefaultMinRAMMB,
		MinDiskMB:      config.DefaultMinDiskMB,
		MinJavaVersion: config.DefaultMinJavaVersion,
	}
}

func TestSettingsValidate_AcceptsFullyPopulatedSettings(t *testing.T) {
	require.NoError(t, validSettings().Validate(), "fully populated defaults must pass Validate")
}

func TestSettingsValidate_RejectsInvalidPortAndMemory(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*Settings)
	}{
		{"zero port", func(s *Settings) { s.Port = 0 }},
		{"negative port", func(s *Settings) { s.Port = -1 }},
		{"port too high", func(s *Settings) { s.Port = 65536 }},
		{"zero memory", func(s *Settings) { s.Memory = 0 }},
		{"negative memory", func(s *Settings) { s.Memory = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSettings()
			tt.mutate(s)
			assert.Error(t, s.Validate(), "%s must fail Validate", tt.name)
		})
	}
}

func TestSettingsValidate_RejectsZeroOrNegativeThresholds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Settings)
	}{
		{"zero MinRAMMB", func(s *Settings) { s.MinRAMMB = 0 }},
		{"negative MinRAMMB", func(s *Settings) { s.MinRAMMB = -1 }},
		{"zero MinDiskMB", func(s *Settings) { s.MinDiskMB = 0 }},
		{"negative MinDiskMB", func(s *Settings) { s.MinDiskMB = -1 }},
		{"zero MinJavaVersion", func(s *Settings) { s.MinJavaVersion = 0 }},
		{"negative MinJavaVersion", func(s *Settings) { s.MinJavaVersion = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSettings()
			tt.mutate(s)
			assert.Error(t, s.Validate(), "%s must fail Validate so misconfigured pre-flight thresholds cannot start a session", tt.name)
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

	settings := &Settings{
		Port:           25565,
		Memory:         4096,
		StartScript:    DefaultStartScript,
		MinRAMMB:       config.DefaultMinRAMMB,
		MinDiskMB:      config.DefaultMinDiskMB,
		MinJavaVersion: config.DefaultMinJavaVersion,
	}
	err := settings.Save()
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tempDir, SettingsFilename))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	expected := fmt.Sprintf(`{
  "port": 25565,
  "memory": 4096,
  "start_script": "start.bat",
  "min_ram_mb": %d,
  "min_disk_mb": %d,
  "min_java_version": %d,
  "local_retention": {
    "keep_last": 0,
    "keep_daily": 0,
    "keep_weekly": 0,
    "keep_monthly": 0
  },
  "remote_retention": {
    "keep_last": 0,
    "keep_daily": 0,
    "keep_weekly": 0,
    "keep_monthly": 0
  },
  "loaded_ref_id": ""
}`, config.DefaultMinRAMMB, config.DefaultMinDiskMB, config.DefaultMinJavaVersion)
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

func TestLoadSettings_BackfillsMissingThresholds(t *testing.T) {
	tempDir := t.TempDir()
	originalRootPath := config.RootPath
	config.RootPath = tempDir
	defer func() { config.RootPath = originalRootPath }()

	v1 := []byte(`{"port":25570,"memory":8192}`)
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, SettingsFilename), v1, 0o644),
		"seed a v1 settings.json (no min_* fields) so the loader can be exercised")

	loaded, err := LoadSettings()
	require.NoError(t, err, "LoadSettings must succeed against a v1 settings.json after a binary upgrade")

	assert.Equal(t, 25570, loaded.Port, "explicit fields from disk must survive backfill")
	assert.Equal(t, 8192, loaded.Memory, "explicit fields from disk must survive backfill")
	assert.Equal(t, config.DefaultMinRAMMB, loaded.MinRAMMB, "missing MinRAMMB must be backfilled to the documented default")
	assert.Equal(t, config.DefaultMinDiskMB, loaded.MinDiskMB, "missing MinDiskMB must be backfilled to the documented default")
	assert.Equal(t, config.DefaultMinJavaVersion, loaded.MinJavaVersion, "missing MinJavaVersion must be backfilled to the documented default")
	assert.NoError(t, loaded.Validate(), "backfilled settings must pass Validate so users do not see errors after upgrade")
}
