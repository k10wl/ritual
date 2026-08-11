package adapters

import (
	"context"
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCmdBuilder struct {
	label string
}

func (f fakeCmdBuilder) Build(context.Context, io.Reader, io.Writer) (*exec.Cmd, error) {
	return exec.Command(f.label), nil
}

func TestSwappableCmdBuilder_ForwardsToCurrentBackingBuilder(t *testing.T) {
	sw := NewSwappableCmdBuilder()
	sw.Store(fakeCmdBuilder{label: "first"})

	cmd, err := sw.Build(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "first", cmd.Path, "Build must forward to the currently stored backing builder")
}

func TestSwappableCmdBuilder_SwapMidTest_LaterBuildObservesNewBackingBuilder(t *testing.T) {
	sw := NewSwappableCmdBuilder()
	sw.Store(fakeCmdBuilder{label: "first"})

	first, err := sw.Build(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "first", first.Path)

	// A caller holding only the facade (e.g. running.Strategy, which stores
	// this as a plain ports.CmdBuilder interface value once at boot) must
	// observe the swapped builder on its next Build call, with no
	// reconstruction of the caller itself (design-log/055 Phase D).
	sw.Store(fakeCmdBuilder{label: "second"})

	second, err := sw.Build(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "second", second.Path, "Build must observe the newly stored builder immediately after Store")
}
