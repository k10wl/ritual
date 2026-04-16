package mocks

import (
	"context"
	"io"
	"os/exec"

	"ritual/internal/core/ports"
)

type MockCmdBuilder struct {
	BuildFunc func(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error)
}

var _ ports.CmdBuilder = (*MockCmdBuilder)(nil)

func (m *MockCmdBuilder) Build(ctx context.Context, stdin io.Reader, stdout io.Writer) (*exec.Cmd, error) {
	if m.BuildFunc != nil {
		return m.BuildFunc(ctx, stdin, stdout)
	}
	return nil, nil
}
