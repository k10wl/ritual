// Package logging attaches a stdout + optional-file subscriber to the
// event bus. Replaces the ad-hoc consumer goroutine that used to live
// in cmd/cli.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ritual/internal/config"
	"ritual/internal/core/ports"
)

// Attach subscribes a formatter to bus. Returns a stop func that
// cancels the subscription and waits for the goroutine to drain.
func Attach(bus ports.EventBus, logFile io.Writer) func() {
	out := io.Writer(os.Stdout)
	if logFile != nil {
		out = io.MultiWriter(os.Stdout, logFile)
	}
	ch, cancel := bus.Subscribe()
	var wg sync.WaitGroup
	wg.Go(func() {
		for e := range ch {
			write(out, e)
		}
	})
	return func() {
		cancel()
		wg.Wait()
	}
}

// CreateLogFile opens a timestamped log file under {workRoot}/logs.
// Returns the file and a close helper. Caller owns the close.
func CreateLogFile(workRoot *os.Root) (*os.File, func(), error) {
	rootPath := workRoot.Name()
	logsDir := filepath.Join(rootPath, config.LogsDir)
	if err := os.MkdirAll(logsDir, config.DirPermission); err != nil {
		return nil, nil, fmt.Errorf("create logs dir: %w", err)
	}
	path := filepath.Join(logsDir, time.Now().Format(config.TimestampFormat)+config.LogExtension)
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("create log file: %w", err)
	}
	return f, func() { _ = f.Close() }, nil
}

func stamp() string { return time.Now().Format("15:04:05") }

func write(w io.Writer, evt ports.Event) {
	switch e := evt.(type) {
	case ports.StartInfo:
		fmt.Fprintf(w, "[%s] [%s] Starting...\n", stamp(), e.Operation)
	case ports.UpdateInfo:
		writeUpdate(w, e)
	case ports.FinishInfo:
		fmt.Fprintf(w, "[%s] [%s] Completed\n", stamp(), e.Operation)
	case ports.ErrorInfo:
		fmt.Fprintf(w, "[%s] [%s] ERROR: %v\n", stamp(), e.Operation, e.Err)
	case ports.StateChangedInfo:
		fmt.Fprintf(w, "[%s] %s → %s\n", stamp(), e.From, e.To)
	case ports.StateFailedInfo:
		fmt.Fprintf(w, "[%s] FAILED in %s: %v\n", stamp(), e.State, e.Err)
	case ports.RetryAttemptInfo:
		writeRetry(w, e)
	default:
		fmt.Fprintf(w, "[%s] %v\n", stamp(), evt)
	}
}

func writeUpdate(w io.Writer, e ports.UpdateInfo) {
	if e.Data == nil {
		fmt.Fprintf(w, "[%s] [%s] %s\n", stamp(), e.Operation, e.Message)
		return
	}
	if pct, ok := e.Data["percent"]; ok {
		fmt.Fprintf(w, "[%s] [%s] %s (%.1f%%)\n", stamp(), e.Operation, e.Message, pct)
		return
	}
	fmt.Fprintf(w, "[%s] [%s] %s %v\n", stamp(), e.Operation, e.Message, e.Data)
}

func writeRetry(w io.Writer, e ports.RetryAttemptInfo) {
	if e.Key != "" {
		fmt.Fprintf(w, "[%s] [retry] %s key=%s attempt=%d err=%v\n", stamp(), e.Operation, e.Key, e.Attempt, e.Err)
		return
	}
	fmt.Fprintf(w, "[%s] [retry] %s attempt=%d err=%v\n", stamp(), e.Operation, e.Attempt, e.Err)
}
