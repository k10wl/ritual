// Package logging attaches a stdout + optional-file subscriber to the
// event bus.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"ritual/internal/config"
	"ritual/internal/core/ports"
	"ritual/internal/core/ritual"
	"sync"
	"time"
)

// Attach subscribes a formatter to bus. Returns a stop func that
// cancels the subscription and waits for the goroutine to drain.
//
// The file sink is written independently of stdout — NOT via io.MultiWriter.
// MultiWriter short-circuits on the first writer's error, and in a -H windowsgui
// release build os.Stdout is a dead handle whose Write fails, which previously
// left <root>/logs/<ts>.log empty for every shipped build (defeating audit
// fix #6). Writing the file first guarantees the on-disk record; stdout is
// best-effort for the console/dev path.
func Attach(bus ports.EventBus, logFile io.Writer) func() {
	ch, cancel := bus.Subscribe()
	var wg sync.WaitGroup
	wg.Go(func() {
		for e := range ch {
			if logFile != nil {
				write(logFile, e)
			}
			write(os.Stdout, e)
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
	f, err := os.Create(path) // #nosec G304 -- path derived from workRoot + timestamp
	if err != nil {
		return nil, nil, fmt.Errorf("create log file: %w", err)
	}
	return f, func() { _ = f.Close() }, nil
}

// Build opens <workRoot>/logs/<ts>.log and subscribes a bus formatter
// against it. Returned stop detaches the formatter, drains its goroutine,
// and closes the file. One call wires the entire on-disk log story —
// composition roots (cmd/gui, integration test setup) call Build so a
// future drift between them cannot leave a session unlogged.
//
// Audit fix #6 (docs/dev-session-2026-04-25-poc-setup.md): pre-fix
// CreateLogFile + Attach were separately exposed but the GUI composition
// root never called either, so a session left no on-disk record.
func Build(bus ports.EventBus, workRoot *os.Root) (func(), error) {
	f, closeFile, err := CreateLogFile(workRoot)
	if err != nil {
		return nil, err
	}
	detach := Attach(bus, f)
	return func() {
		detach()
		closeFile()
	}, nil
}

func stamp() string { return time.Now().Format("15:04:05") }

func write(w io.Writer, evt ports.Event) {
	switch e := evt.(type) {
	case ritual.StartInfo:
		_, _ = fmt.Fprintf(w, "[%s] [%s] Starting...\n", stamp(), e.Operation)
	case ritual.UpdateInfo:
		writeUpdate(w, e)
	case ritual.FinishInfo:
		_, _ = fmt.Fprintf(w, "[%s] [%s] Completed\n", stamp(), e.Operation)
	case ritual.ErrorInfo:
		_, _ = fmt.Fprintf(w, "[%s] [%s] ERROR: %v\n", stamp(), e.Operation, e.Err)
	case ritual.StateChangedInfo:
		_, _ = fmt.Fprintf(w, "[%s] %s → %s\n", stamp(), e.From, e.To)
	case ritual.StateFailedInfo:
		_, _ = fmt.Fprintf(w, "[%s] FAILED in %s: %v\n", stamp(), e.State, e.Err)
	default:
		_, _ = fmt.Fprintf(w, "[%s] %v\n", stamp(), evt)
	}
}

func writeUpdate(w io.Writer, e ritual.UpdateInfo) {
	if e.Data == nil {
		_, _ = fmt.Fprintf(w, "[%s] [%s] %s\n", stamp(), e.Operation, e.Message)
		return
	}
	if pct, ok := e.Data["percent"]; ok {
		_, _ = fmt.Fprintf(w, "[%s] [%s] %s (%.1f%%)\n", stamp(), e.Operation, e.Message, pct)
		return
	}
	_, _ = fmt.Fprintf(w, "[%s] [%s] %s %v\n", stamp(), e.Operation, e.Message, e.Data)
}
