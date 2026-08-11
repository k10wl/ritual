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

// Store swaps the backing builder. inner must not be nil.
func (s *SwappableCmdBuilder) Store(inner ports.CmdBuilder) {
	s.p.Store(&inner)
}

func (s *SwappableCmdBuilder) Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	return (*s.p.Load()).Build(ctx, stdin, stdout)
}

var _ ports.CmdBuilder = (*SwappableCmdBuilder)(nil)
