// Package prompt provides a stdin-backed implementation of
// ports.Prompter for use by services.PromptSettings. Kept out of the
// composition root so it holds one less concrete type.
package prompt

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"ritual/internal/core/ports"
	"strings"
)

type stdinPrompter struct {
	in  *bufio.Reader
	out io.Writer
}

// NewStdin wraps in/out in a Prompter. Empty input or read errors
// return def.
func NewStdin(in io.Reader, out io.Writer) ports.Prompter {
	return &stdinPrompter{in: bufio.NewReader(in), out: out}
}

func (p *stdinPrompter) Prompt(_ context.Context, _, prompt, def string) (string, error) {
	if def != "" {
		_, _ = fmt.Fprintf(p.out, "%s [%s]: ", prompt, def)
	} else {
		_, _ = fmt.Fprintf(p.out, "%s: ", prompt)
	}
	line, err := p.in.ReadString('\n')
	if err != nil {
		return def, nil //nolint:nilerr // read errors fall back to the default by design
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	_, _ = fmt.Fprintln(p.out, line)
	return line, nil
}
