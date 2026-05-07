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

func TestDefaultSettings_RemoteR2IsNil_DefaultIsMockMode(t *testing.T) {
	settings := DefaultSettings()
	assert.Nil(t, settings.RemoteR2, "RemoteR2 must default to nil so a freshly-installed alpha picks the local-FS mock remote without configuring anything — operators flip to real R2 by writing the four credential fields into settings.json")
}

func TestLoadSettings_RemoteR2RoundTrips_AllFourCredentialFieldsSurvive(t *testing.T) {
	tempDir := t.TempDir()
	originalRootPath := config.RootPath
	config.RootPath = tempDir
	defer func() { config.RootPath = originalRootPath }()

	saved := DefaultSettings()
	saved.RemoteR2 = &R2Config{
		Bucket:          "ritual-alpha",
		AccountID:       "acc-123",
		AccessKeyID:     "ak-456",
		SecretAccessKey: "sk-789",
	}
	require.NoError(t, saved.Save(), "Save must succeed against a settings struct carrying a populated RemoteR2 — round-trip is the contract operators rely on after editing settings.json")

	loaded, err := LoadSettings()
	require.NoError(t, err, "LoadSettings must succeed against the just-written file — corrupt JSON would block startup")
	require.NotNil(t, loaded.RemoteR2, "RemoteR2 must survive a Save→Load cycle so the factory can detect 'real R2 configured' on the next boot")
	assert.Equal(t, "ritual-alpha", loaded.RemoteR2.Bucket, "Bucket must round-trip verbatim — typos here surface as 404s the operator will struggle to debug")
	assert.Equal(t, "acc-123", loaded.RemoteR2.AccountID, "AccountID must round-trip verbatim — drives the R2 endpoint URL via R2EndpointFormat")
	assert.Equal(t, "ak-456", loaded.RemoteR2.AccessKeyID, "AccessKeyID must round-trip verbatim — half the auth pair")
	assert.Equal(t, "sk-789", loaded.RemoteR2.SecretAccessKey, "SecretAccessKey must round-trip verbatim — the other half; alpha stage accepts plaintext, future revisions can layer encryption")
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
  }
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
