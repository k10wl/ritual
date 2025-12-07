package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"ritual/internal/core/ports"
)

// timestamp returns current time in HH:MM:SS format
func timestamp() string {
	return time.Now().Format("15:04:05")
}

// consumeEvents reads events from channel and prints to stdout and optional log file
// Runs until channel is closed
func consumeEvents(events <-chan ports.Event, logFile io.Writer) {
	reader := bufio.NewReader(os.Stdin)

	// Create writer that outputs to both stdout and log file
	var writer io.Writer = os.Stdout
	if logFile != nil {
		writer = io.MultiWriter(os.Stdout, logFile)
	}

	for evt := range events {
		switch e := evt.(type) {
		case ports.StartEvent:
			fmt.Fprintf(writer, "[%s] [%s] Starting...\n", timestamp(), e.Operation)
		case ports.UpdateEvent:
			if e.Data != nil {
				if pct, ok := e.Data["percent"]; ok {
					fmt.Fprintf(writer, "[%s] [%s] %s (%.1f%%)\n", timestamp(), e.Operation, e.Message, pct)
				} else {
					fmt.Fprintf(writer, "[%s] [%s] %s %v\n", timestamp(), e.Operation, e.Message, e.Data)
				}
			} else {
				fmt.Fprintf(writer, "[%s] [%s] %s\n", timestamp(), e.Operation, e.Message)
			}
		case ports.FinishEvent:
			fmt.Fprintf(writer, "[%s] [%s] Completed\n", timestamp(), e.Operation)
		case ports.ErrorEvent:
			fmt.Fprintf(writer, "[%s] [%s] ERROR: %v\n", timestamp(), e.Operation, e.Err)
		case ports.PromptEvent:
			handlePrompt(reader, e, writer)
		}
	}
}

// handlePrompt displays prompt and sends user response back via channel
func handlePrompt(reader *bufio.Reader, e ports.PromptEvent, writer io.Writer) {
	if e.DefaultValue != "" {
		fmt.Fprintf(writer, "%s [%s]: ", e.Prompt, e.DefaultValue)
	} else {
		fmt.Fprintf(writer, "%s: ", e.Prompt)
	}

	input, err := reader.ReadString('\n')
	if err != nil {
		e.ResponseChan <- any(e.DefaultValue)
		return
	}

	input = strings.TrimSpace(input)
	if input == "" {
		e.ResponseChan <- any(e.DefaultValue)
	} else {
		fmt.Fprintf(writer, "%s\n", input)
		e.ResponseChan <- any(input)
	}
}
