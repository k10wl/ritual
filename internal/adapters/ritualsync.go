package adapters

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"strings"

	"ritual/internal/core/ports"
)

const ritualSyncFile = ".ritualsync"

// ParseRitualSync reads .ritualsync from fsys and returns a path filter function.
// Returns error if file is missing, empty, or contains path traversal.
func ParseRitualSync(fsys fs.FS) (func(string) bool, error) {
	f, err := fsys.Open(ritualSyncFile)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", ritualSyncFile, err)
	}
	defer f.Close()

	var rules []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "..") {
			return nil, fmt.Errorf("path traversal in %s: %s", ritualSyncFile, line)
		}
		rules = append(rules, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", ritualSyncFile, err)
	}
	if len(rules) == 0 {
		return nil, errors.New(ritualSyncFile + " is empty — explicit sync rules required")
	}

	// Wildcard — match everything
	for _, r := range rules {
		if r == "*" {
			return func(string) bool { return true }, nil
		}
	}

	return func(path string) bool {
		for _, rule := range rules {
			if strings.HasSuffix(rule, "/") {
				if strings.HasPrefix(path, rule) {
					return true
				}
			} else {
				if path == rule {
					return true
				}
			}
		}
		return false
	}, nil
}

// scannerFunc adapts a function to the DirectoryScanner interface.
type scannerFunc func(context.Context) (map[string]string, error)

func (f scannerFunc) Scan(ctx context.Context) (map[string]string, error) { return f(ctx) }

// NewFilteredScanner wraps a scanner with a path filter.
// .ritualsync itself always passes (exempt from its own filter).
func NewFilteredScanner(inner ports.DirectoryScanner, filter func(string) bool) ports.DirectoryScanner {
	return scannerFunc(func(ctx context.Context) (map[string]string, error) {
		m, err := inner.Scan(ctx)
		if err != nil {
			return nil, err
		}
		maps.DeleteFunc(m, func(path, _ string) bool {
			return path != ritualSyncFile && !filter(path)
		})
		return m, nil
	})
}
