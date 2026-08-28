// Package preprundup measures how long the two invisible session beats —
// server prep (Acquiring → ServerReady) and server wrap
// (ServerStopping → Done) — actually take on this machine, and turns that
// history into an ETA the dial can show. Design-log/027 (+2026-08-28
// addendum).
package preprundup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"ritual/internal/config"
)

// HistoryFilename is the on-disk filename for the prep/wrap timing history,
// stored beside settings.json/lock at config.RootPath (design-log/027 §Q16
// addendum — the "functional folder", not the movable WorkRoot).
const HistoryFilename = "prep-history.json"

const historyVersion = 1

// Sample is one completed session's prep + wrap timing.
type Sample struct {
	RunID     string `json:"runID"`
	StartedAt string `json:"startedAt"` // RFC3339
	PrepMs    int64  `json:"prepMs"`
	WrapMs    int64  `json:"wrapMs"`
}

// File is the on-disk shape of prep-history.json. Last is nil until the
// first FlowSession run completes; design-log/058's "just store last one"
// deviation (2026-08-28) replaced the original trimmed-mean-over-10 design
// with the single most recently completed run — no averaging, no ring
// buffer, and a fresh estimate is available after just one session instead
// of two (the old estimator required ≥2 samples before returning non-zero).
type File struct {
	Version int     `json:"version"`
	Last    *Sample `json:"last,omitempty"`
}

// Store loads and durably appends timing samples. HistoryPath is a plain
// pretty-printed JSON file (user directive) — not merged into settings.json,
// so a user can delete it to "forget my machine" without losing config.
type Store struct {
	path string
}

// NewStore returns a Store rooted at config.RootPath.
func NewStore() *Store {
	return &Store{path: HistoryPath()}
}

// HistoryPath returns the absolute path to prep-history.json.
func HistoryPath() string {
	return filepath.Join(config.RootPath, HistoryFilename)
}

// Load reads the history file. A missing file is not an error — it returns
// an empty File{Version: historyVersion} (first run on this machine).
func (s *Store) Load() (File, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return File{Version: historyVersion}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("read prep history: %w", err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("parse prep history: %w", err)
	}
	return f, nil
}

// Append overwrites the previous sample with this one and writes atomically
// (temp + fsync + rename, mirroring domain.Settings.Save — design-log/055).
// Only ever one sample survives — design-log/058's "just store last one".
func (s *Store) Append(sample Sample) error {
	f, err := s.Load()
	if err != nil {
		return err
	}
	f.Version = historyVersion
	f.Last = &sample
	return s.save(f)
}

func (s *Store) save(f File) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal prep history: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, config.DirPermission); err != nil {
		return fmt.Errorf("mkdir prep history dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".prep-history-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp prep history file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp prep history file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsync temp prep history file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp prep history file: %w", err)
	}
	if err := os.Chmod(tmpPath, config.FilePermission); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp prep history file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		cleanup()
		return fmt.Errorf("rename prep history file into place: %w", err)
	}
	return nil
}
