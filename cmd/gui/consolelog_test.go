package main

import (
	"context"
	"os"
	"path/filepath"
	"ritual/internal/config"
	"strings"
	"testing"
)

func writeLog(t *testing.T, dir, body string) {
	t.Helper()
	logsDir := filepath.Join(dir, config.LogsDir)
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "latest.log"), []byte(body), 0o644); err != nil {
		t.Fatalf("write latest.log: %v", err)
	}
}

func TestTailLines_TailAndTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "a\nb\nc\nd\n") // trailing newline → no blank final row
	got, err := tailLines(filepath.Join(dir, config.LogsDir, "latest.log"), 2)
	if err != nil {
		t.Fatalf("tailLines: %v", err)
	}
	if want := []string{"c", "d"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tail = %v, want %v", got, want)
	}
}

func TestTailLines_FewerThanCap(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "only\nthese\n")
	got, err := tailLines(filepath.Join(dir, config.LogsDir, "latest.log"), 1024)
	if err != nil {
		t.Fatalf("tailLines: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%v)", len(got), got)
	}
}

func TestTailLines_MissingFileIsNotAnError(t *testing.T) {
	got, err := tailLines(filepath.Join(t.TempDir(), "logs", "latest.log"), 10)
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if got != nil {
		t.Fatalf("missing file should return nil, got %v", got)
	}
}

func TestNewConsoleReader_ResolvesCwdFromStartScript(t *testing.T) {
	serverPath := t.TempDir()
	// Start script in an instance subfolder ⇒ server cwd is that subfolder, so
	// latest.log lives under <serverPath>/instance/logs (design-log/043 §Q5).
	instance := filepath.Join(serverPath, "instance")
	writeLog(t, instance, "boot\nready\n")

	reader := newConsoleReader(serverPath, filepath.Join("instance", "run.sh"))
	got, err := reader(context.Background())
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	if want := "boot,ready"; strings.Join(got, ",") != want {
		t.Fatalf("lines = %v, want %v", got, want)
	}
}

func TestNewConsoleReader_FlatStartScript(t *testing.T) {
	serverPath := t.TempDir()
	writeLog(t, serverPath, "x\ny\nz\n")
	reader := newConsoleReader(serverPath, "start.bat")
	got, err := reader(context.Background())
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (%v)", len(got), got)
	}
}
