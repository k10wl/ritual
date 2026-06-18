// Package adapters provides concrete implementations of the core ports.
package adapters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"strconv"
	"strings"
	"sync"
)

var _ ports.CmdBuilder = (*ServerCmdBuilder)(nil)

// ServerCmdBuilder composes the server start command from a start script and runtime settings.
type ServerCmdBuilder struct {
	serverPath  string
	startScript string
	runtime     func() (*domain.ServerRuntime, error)

	mu       sync.Mutex
	workRoot *os.Root // lazily opened on first Build (design-log/040)
}

// NewServerCmdBuilder validates inputs and returns a ServerCmdBuilder ready to build commands.
//
// serverPath is the server sandbox directory; it is NOT created or opened here.
// The os.Root is opened lazily on first Build, so a fresh host carries no empty
// server/ folder until an Apply has actually written into it (design-log/040).
func NewServerCmdBuilder(serverPath string, startScript string, runtime func() (*domain.ServerRuntime, error)) (*ServerCmdBuilder, error) {
	if serverPath == "" {
		return nil, errors.New("server path cannot be empty")
	}
	if startScript == "" {
		return nil, errors.New("start script cannot be empty")
	}
	if runtime == nil {
		return nil, errors.New("runtime factory cannot be nil")
	}

	return &ServerCmdBuilder{
		serverPath:  serverPath,
		startScript: startScript,
		runtime:     runtime,
	}, nil
}

// root opens the server sandbox lazily on first launch and caches it.
//
// It never MkdirAll's: when server/ is absent (e.g. a fresh-host skip-sync run
// with no prior Apply) it surfaces an honest error and leaves nothing behind —
// the dir is materialised only by the Apply that writes into it (design-log/040
// Q3). os.OpenRoot does not create its target, so an absent dir fails here.
func (b *ServerCmdBuilder) root() (*os.Root, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.workRoot != nil {
		return b.workRoot, nil
	}
	root, err := os.OpenRoot(b.serverPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no server files at %s — run a sync first", b.serverPath)
		}
		return nil, fmt.Errorf("open server dir: %w", err)
	}
	b.workRoot = root
	return root, nil
}

// Close releases the lazily-opened server-sandbox os.Root. Safe to call when
// Build was never invoked (workRoot still nil) and idempotent. On Windows the
// open directory handle blocks removal of the sandbox dir, so tests (and any
// caller that tears a builder down before process exit) must release it.
func (b *ServerCmdBuilder) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.workRoot == nil {
		return nil
	}
	err := b.workRoot.Close()
	b.workRoot = nil
	return err
}

// Build reads the start script and returns an *exec.Cmd configured with runtime memory/port.
func (b *ServerCmdBuilder) Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	server, err := b.runtime()
	if err != nil {
		return nil, fmt.Errorf("resolve runtime: %w", err)
	}

	workRoot, err := b.root()
	if err != nil {
		return nil, err
	}

	f, err := workRoot.Open(b.startScript)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("start script not found at %s", b.startScript)
		}
		return nil, fmt.Errorf("open start script at %s: %w", b.startScript, err)
	}
	defer func() { _ = f.Close() }()

	content, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read start script %s: %w", b.startScript, err)
	}

	memoryArg := "-Xmx" + strconv.Itoa(server.Memory) + "M"
	line := strings.ReplaceAll(strings.TrimSpace(string(content)), "%1", memoryArg)
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty start script %s", b.startScript)
	}
	parts = append(parts, "--port", strconv.Itoa(server.Port))

	scriptPath := filepath.Join(workRoot.Name(), b.startScript)
	// #nosec G204 -- parts sourced from project-controlled start script, not user input
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stdout

	return cmd, nil
}
