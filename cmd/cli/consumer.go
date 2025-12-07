package main

import (
	"fmt"
	"ritual/internal/core/ports"
)

// consumeEvents reads events from channel and prints to stdout
// Runs until channel is closed
func consumeEvents(events <-chan ports.Event) {
	for evt := range events {
		switch e := evt.(type) {
		case ports.StartEvent:
			fmt.Printf("[%s] Starting...\n", e.Operation)
		case ports.UpdateEvent:
			if e.Data != nil {
				if pct, ok := e.Data["percent"]; ok {
					fmt.Printf("[%s] %s (%.1f%%)\n", e.Operation, e.Message, pct)
				} else {
					fmt.Printf("[%s] %s %v\n", e.Operation, e.Message, e.Data)
				}
			} else {
				fmt.Printf("[%s] %s\n", e.Operation, e.Message)
			}
		case ports.FinishEvent:
			fmt.Printf("[%s] Completed\n", e.Operation)
		case ports.ErrorEvent:
			fmt.Printf("[%s] ERROR: %v\n", e.Operation, e.Err)
		}
	}
}
