package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"ritual/internal/config"
)

const (
	envProfile  = "RITUAL_ENV"
	envBucket   = "RITUAL_R2_BUCKET"
	envSrcKey   = "R2_BUCKET_NAME"
	envMode     = "RITUAL_REMOTE_MODE"
	envAccount  = "RITUAL_R2_ACCOUNT_ID"
	envSrcAccID = "R2_ACCOUNT_ID"
)

// LoadEnvFiles reads .env.{RITUAL_ENV}.local from CWD; the test chdir's to
// a TempDir so the loader's fallback path is the temp file and the
// executable-adjacent candidate (the test binary's dir) cannot collide.

func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	return dir
}

func writeEnvFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// clearEnv unsets each key for the duration of the test. t.Setenv("", "")
// would leave the key present-but-empty, which godotenv treats as already
// set and refuses to overwrite — so use real unset + Cleanup restore.
func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		old, had := os.LookupEnv(k)
		_ = os.Unsetenv(k)
		if had {
			t.Cleanup(func() { _ = os.Setenv(k, old) })
		} else {
			t.Cleanup(func() { _ = os.Unsetenv(k) })
		}
	}
}

func TestLoadEnvFiles_DefaultProfileReadsDevFile(t *testing.T) {
	dir := chdirTemp(t)
	clearEnv(t, envProfile, envBucket, envSrcKey)
	writeEnvFile(t, dir, ".env.dev.local", "RITUAL_R2_BUCKET=devbucket\n")

	config.LoadEnvFiles()

	if got := os.Getenv(envBucket); got != "devbucket" {
		t.Fatalf("RITUAL_R2_BUCKET = %q, want devbucket (default profile must load .env.dev.local)", got)
	}
}

func TestLoadEnvFiles_ProfileSelectsFile(t *testing.T) {
	dir := chdirTemp(t)
	clearEnv(t, envBucket, envSrcKey)
	t.Setenv(envProfile, "prod")
	writeEnvFile(t, dir, ".env.dev.local", "RITUAL_R2_BUCKET=devbucket\n")
	writeEnvFile(t, dir, ".env.prod.local", "RITUAL_R2_BUCKET=prodbucket\n")

	config.LoadEnvFiles()

	if got := os.Getenv(envBucket); got != "prodbucket" {
		t.Fatalf("RITUAL_R2_BUCKET = %q, want prodbucket (RITUAL_ENV=prod must pick .env.prod.local, not .env.dev.local)", got)
	}
}

func TestLoadEnvFiles_MissingFileIsNoop(t *testing.T) {
	chdirTemp(t)
	clearEnv(t, envProfile, envBucket, envSrcKey)

	config.LoadEnvFiles()

	if got := os.Getenv(envBucket); got != "" {
		t.Fatalf("RITUAL_R2_BUCKET = %q, want empty (missing .env.*.local must be non-fatal)", got)
	}
}

func TestLoadEnvFiles_ShellEnvBeatsFile(t *testing.T) {
	dir := chdirTemp(t)
	clearEnv(t, envProfile, envSrcKey)
	t.Setenv(envBucket, "from-shell")
	writeEnvFile(t, dir, ".env.dev.local", "RITUAL_R2_BUCKET=from-file\n")

	config.LoadEnvFiles()

	if got := os.Getenv(envBucket); got != "from-shell" {
		t.Fatalf("RITUAL_R2_BUCKET = %q, want from-shell (operator shell exports must beat .env.*.local for CI safety)", got)
	}
}

func TestLoadEnvFiles_RemapsCloudflareDashboardNames(t *testing.T) {
	dir := chdirTemp(t)
	clearEnv(t, envProfile, envBucket, envAccount, envSrcKey, envSrcAccID)
	writeEnvFile(t, dir, ".env.dev.local",
		"R2_BUCKET_NAME=mapped-bucket\nR2_ACCOUNT_ID=mapped-account\n")

	config.LoadEnvFiles()

	if got := os.Getenv(envBucket); got != "mapped-bucket" {
		t.Fatalf("RITUAL_R2_BUCKET = %q, want mapped-bucket (loader must mirror Taskfile R2_* → RITUAL_R2_* remap so the same .env.*.local serves both Task and the shipped binary)", got)
	}
	if got := os.Getenv(envAccount); got != "mapped-account" {
		t.Fatalf("RITUAL_R2_ACCOUNT_ID = %q, want mapped-account", got)
	}
}

func TestLoadEnvFiles_RemapDoesNotOverwriteExistingRuntimeName(t *testing.T) {
	dir := chdirTemp(t)
	clearEnv(t, envProfile, envSrcKey)
	t.Setenv(envBucket, "from-shell")
	writeEnvFile(t, dir, ".env.dev.local", "R2_BUCKET_NAME=mapped-bucket\n")

	config.LoadEnvFiles()

	if got := os.Getenv(envBucket); got != "from-shell" {
		t.Fatalf("RITUAL_R2_BUCKET = %q, want from-shell (remap must respect an already-set runtime name)", got)
	}
}
