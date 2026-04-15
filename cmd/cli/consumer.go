package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"ritual/internal/core/ports"
)

// timestamp returns current time in HH:MM:SS format
func timestamp() string {
	return time.Now().Format("15:04:05")
}

// consumeEvents reads events from the bus subscription channel and writes to
// stdout plus the optional log file. Runs until the channel is closed (i.e.
// the subscribe cancel func was invoked).
func consumeEvents(events <-chan ports.Event, logFile io.Writer) {
	var writer io.Writer = os.Stdout
	if logFile != nil {
		writer = io.MultiWriter(os.Stdout, logFile)
	}

	for evt := range events {
		switch e := evt.(type) {
		case ports.StartInfo:
			fmt.Fprintf(writer, "[%s] [%s] Starting...\n", timestamp(), e.Operation)
		case ports.UpdateInfo:
			if e.Data != nil {
				if pct, ok := e.Data["percent"]; ok {
					fmt.Fprintf(writer, "[%s] [%s] %s (%.1f%%)\n", timestamp(), e.Operation, e.Message, pct)
				} else {
					fmt.Fprintf(writer, "[%s] [%s] %s %v\n", timestamp(), e.Operation, e.Message, e.Data)
				}
			} else {
				fmt.Fprintf(writer, "[%s] [%s] %s\n", timestamp(), e.Operation, e.Message)
			}
		case ports.FinishInfo:
			fmt.Fprintf(writer, "[%s] [%s] Completed\n", timestamp(), e.Operation)
		case ports.ErrorInfo:
			fmt.Fprintf(writer, "[%s] [%s] ERROR: %v\n", timestamp(), e.Operation, e.Err)
		case ports.StateChangedInfo:
			fmt.Fprintf(writer, "[%s] %s → %s\n", timestamp(), e.From, e.To)
		case ports.StateFailedInfo:
			fmt.Fprintf(writer, "[%s] FAILED in %s: %v\n", timestamp(), e.State, e.Err)
		case ports.RetryAttemptInfo:
			if e.Key != "" {
				fmt.Fprintf(writer, "[%s] [retry] %s key=%s attempt=%d err=%v\n", timestamp(), e.Operation, e.Key, e.Attempt, e.Err)
			} else {
				fmt.Fprintf(writer, "[%s] [retry] %s attempt=%d err=%v\n", timestamp(), e.Operation, e.Attempt, e.Err)
			}
		default:
			fmt.Fprintf(writer, "[%s] %v\n", timestamp(), evt)
		}
	}
}
