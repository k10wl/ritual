package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"ritual/internal/core/ports"
)

// stdinPrompter reads user input from stdin. Writes the prompt (with optional
// default hint) to the provided writer; empty line or read error returns the
// default, preserving the original consumer.handlePrompt semantics.
type stdinPrompter struct {
	in  *bufio.Reader
	out io.Writer
}

func newStdinPrompter(in io.Reader, out io.Writer) ports.Prompter {
	return &stdinPrompter{in: bufio.NewReader(in), out: out}
}

func (p *stdinPrompter) Prompt(_ context.Context, _, prompt, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", prompt, def)
	} else {
		fmt.Fprintf(p.out, "%s: ", prompt)
	}
	line, err := p.in.ReadString('\n')
	if err != nil {
		return def, nil
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	fmt.Fprintln(p.out, line)
	return line, nil
}
