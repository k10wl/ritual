package main

import (
	"context"
	"os"
	"path/filepath"
	"ritual/internal/config"
	"strings"
)

// consoleBackfillLines bounds the one-shot latest.log backfill to the same
// count the console UI retains at the tail (RING_CAPACITY in ritual-logs.ts).
// Reading more would just be trimmed on first paint (design-log/043 §Q6).
const consoleBackfillLines = 1024

// newConsoleReader builds the on-demand backfill reader the logs window calls
// when it opens (design-log/043 §3b). The Minecraft server runs with
// cwd = filepath.Dir(<serverPath>/<startScript>) (see adapters.cmdbuilder), so
// its own log lands at <cwd>/logs/latest.log. We read the tail raw, newest-last,
// no parsing — the UI renders MC's lines as-is (design-log/042). latest.log is
// truncated per server start, so it is exactly the current run.
func newConsoleReader(serverPath, startScript string) func(context.Context) ([]string, error) {
	return func(context.Context) ([]string, error) {
		cwd := filepath.Dir(filepath.Join(serverPath, startScript))
		return tailLines(filepath.Join(cwd, config.LogsDir, "latest.log"), consoleBackfillLines)
	}
}

// tailLines returns the last n lines of the file. A missing file (the loader
// hasn't written latest.log yet, or an exotic start script) is not an error —
// it returns nil so the console opens to live-only (design-log/043 §Q5). Reads
// the whole file then slices; revisit only if logs grow pathologically large.
func tailLines(path string, n int) ([]string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path from config + settings, not user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	// Drop the trailing empty element from a final newline so the backfill
	// doesn't end on a blank row.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
