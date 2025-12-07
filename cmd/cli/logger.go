package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ritual/internal/config"
)

// createLogFile creates a timestamped log file
// Returns the file and cleanup function
func createLogFile(workRoot *os.Root) (*os.File, func(), error) {
	rootPath := workRoot.Name()
	logsDir := filepath.Join(rootPath, config.LogsDir)
	if err := os.MkdirAll(logsDir, config.DirPermission); err != nil {
		return nil, nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Create timestamped log file
	timestamp := time.Now().Format(config.TimestampFormat)
	logPath := filepath.Join(logsDir, timestamp+config.LogExtension)

	file, err := os.Create(logPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create log file: %w", err)
	}

	cleanup := func() {
		file.Close()
	}

	return file, cleanup, nil
}
