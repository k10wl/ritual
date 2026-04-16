package main_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fakerunBin string

func TestMain(m *testing.M) {
	bin := filepath.Join(os.TempDir(), "fakerun_test")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build fakerun: %s\n%s", err, out)
		os.Exit(1)
	}
	fakerunBin = bin
	code := m.Run()
	os.Remove(bin)
	os.Exit(code)
}

func run(t *testing.T, root string, lines ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(fakerunBin, "--root", root)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(out)
		}
		t.Fatalf("exec fakerun: %s", err)
	}
	return 0, string(out)
}

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func writeOp(path, content string) string {
	return fmt.Sprintf(`{"op":"write","path":"%s","data":"%s"}`, path, b64(content))
}

func deleteOp(path string) string {
	return fmt.Sprintf(`{"op":"delete","path":"%s"}`, path)
}

func exitOp(code int) string {
	return fmt.Sprintf(`{"op":"exit","code":%d}`, code)
}

func TestFakerun_WriteCreatesFileWithParentDirs(t *testing.T) {
	root := t.TempDir()
	code, _ := run(t, root, writeOp("a/b/c.txt", "hello"), exitOp(0))

	assert.Equal(t, 0, code,
		"write + exit 0 should exit cleanly")

	data, err := os.ReadFile(filepath.Join(root, "a", "b", "c.txt"))
	require.NoError(t, err,
		"file should exist at a/b/c.txt after write op")
	assert.Equal(t, "hello", string(data),
		"file content should match what was written")
}

func TestFakerun_WriteOverwritesExisting(t *testing.T) {
	root := t.TempDir()
	code, _ := run(t, root,
		writeOp("file.txt", "first"),
		writeOp("file.txt", "second"),
		exitOp(0),
	)

	assert.Equal(t, 0, code,
		"double write + exit 0 should exit cleanly")

	data, err := os.ReadFile(filepath.Join(root, "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "second", string(data),
		"second write should overwrite first")
}

func TestFakerun_DeleteRemovesFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "doomed.txt"), []byte("bye"), 0644))

	code, _ := run(t, root, deleteOp("doomed.txt"), exitOp(0))

	assert.Equal(t, 0, code,
		"delete + exit 0 should exit cleanly")
	assert.NoFileExists(t, filepath.Join(root, "doomed.txt"),
		"file should be gone after delete op")
}

func TestFakerun_DeleteMissingFile_NoCrash(t *testing.T) {
	root := t.TempDir()
	code, _ := run(t, root, deleteOp("nonexistent.txt"), exitOp(0))

	assert.Equal(t, 0, code,
		"deleting nonexistent file should not crash")
}

func TestFakerun_ExitCode0(t *testing.T) {
	root := t.TempDir()
	code, _ := run(t, root, exitOp(0))
	assert.Equal(t, 0, code, "should exit with code 0")
}

func TestFakerun_ExitCode1(t *testing.T) {
	root := t.TempDir()
	code, _ := run(t, root, exitOp(1))
	assert.Equal(t, 1, code, "should exit with code 1")
}

func TestFakerun_StdinEOF_CleanExit(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(fakerunBin, "--root", root)
	cmd.Stdin = strings.NewReader("")
	err := cmd.Run()
	assert.NoError(t, err,
		"empty stdin (EOF) should produce clean exit 0")
}

func TestFakerun_MultipleOpsInSequence(t *testing.T) {
	root := t.TempDir()
	code, _ := run(t, root,
		writeOp("a.txt", "aaa"),
		writeOp("b.txt", "bbb"),
		writeOp("c.txt", "ccc"),
		deleteOp("b.txt"),
		exitOp(0),
	)

	assert.Equal(t, 0, code, "should exit cleanly")
	assert.FileExists(t, filepath.Join(root, "a.txt"), "a.txt should exist")
	assert.NoFileExists(t, filepath.Join(root, "b.txt"), "b.txt should be deleted")
	assert.FileExists(t, filepath.Join(root, "c.txt"), "c.txt should exist")
}

func TestFakerun_InvalidJSON_ExitsNonZero(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(fakerunBin, "--root", root)
	cmd.Stdin = strings.NewReader("not json\n")
	err := cmd.Run()
	assert.Error(t, err, "invalid JSON should cause non-zero exit")
}

func TestFakerun_UnknownOp_ExitsNonZero(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(fakerunBin, "--root", root)
	cmd.Stdin = strings.NewReader(`{"op":"unknown"}` + "\n")
	err := cmd.Run()
	assert.Error(t, err, "unknown op should cause non-zero exit")
}
