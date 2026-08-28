package adapters

import (
	"context"
	"io"
	"os/exec"
	"ritual/internal/core/ports"
	"sync/atomic"
)

// SwappableCmdBuilder implements ports.CmdBuilder by forwarding Build to
// whatever builder was last Store()'d, via an atomic.Pointer. Lets
// running.Strategy hold this facade once at boot (design-log/055 Q4/Phase D)
// while a workroot relocate rebuilds the real ServerCmdBuilder against the
// new server/ path underneath it.
type SwappableCmdBuilder struct {
	p atomic.Pointer[ports.CmdBuilder]
}

// NewSwappableCmdBuilder returns an unset facade — Store must be called
// before Build is used.
func NewSwappableCmdBuilder() *SwappableCmdBuilder {
	return &SwappableCmdBuilder{}
}

// Store swaps the backing builder, closing the outgoing one first if it
// implements io.Closer — ServerCmdBuilder lazily caches an open os.Root on
// its server/ sandbox (Build's first call), and a relocate replacing it via
// ChangeWorkRoot must not leak that handle: on Windows an open directory
// handle blocks removal/relocation of the directory it points at.
func (s *SwappableCmdBuilder) Store(inner ports.CmdBuilder) {
	old := s.p.Swap(&inner)
	if old == nil {
		return
	}
	if closer, ok := (*old).(io.Closer); ok {
		_ = closer.Close()
	}
}

// Close releases the currently active builder's resources, if it implements
// io.Closer. Callers (and tests) that tear this facade down before process
// exit must call it, for the same reason Store closes the outgoing builder.
func (s *SwappableCmdBuilder) Close() error {
	current := s.p.Load()
	if current == nil {
		return nil
	}
	if closer, ok := (*current).(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (s *SwappableCmdBuilder) Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	return (*s.p.Load()).Build(ctx, stdin, stdout)
}

var _ ports.CmdBuilder = (*SwappableCmdBuilder)(nil)
