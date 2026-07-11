// Package mocks provides test doubles for the ports interfaces.
package mocks

import (
	"context"
	"io"
	"os/exec"
	"ritual/internal/core/ports"
)

// MockCmdBuilder is a test double for ports.CmdBuilder.
type MockCmdBuilder struct {
	BuildFunc func(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error)
}

var _ ports.CmdBuilder = (*MockCmdBuilder)(nil)

// Build calls BuildFunc when set, otherwise returns (nil, nil).
func (m *MockCmdBuilder) Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	if m.BuildFunc != nil {
		return m.BuildFunc(ctx, stdin, stdout)
	}
	return nil, nil //nolint:nilnil // mock default: no cmd, no error
}
