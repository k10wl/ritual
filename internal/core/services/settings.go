package services

import (
	"fmt"
	"net"
	"strconv"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// PromptSettings loads existing settings and prompts user for each value via events
// Returns validated and saved settings
func PromptSettings(events chan<- ports.Event) (*domain.Settings, error) {
	settings, err := domain.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("failed to load existing settings: %w", err)
	}

	ports.SendEvent(events, ports.StartEvent{Operation: "Settings"})
	ports.SendEvent(events, ports.UpdateEvent{
		Operation: "Settings",
		Message:   "Press Enter to accept default values shown in brackets",
	})

	// Prompt for IP
	ip, err := promptWithValidation(events, "IP Address", settings.IP, validateIP)
	if err != nil {
		return nil, err
	}
	settings.IP = ip

	// Prompt for Port
	portStr, err := promptWithValidation(events, "Port", strconv.Itoa(settings.Port), validatePort)
	if err != nil {
		return nil, err
	}
	settings.Port, _ = strconv.Atoi(portStr)

	// Prompt for Memory (display in GB, store in MB)
	memGB := settings.Memory / 1024
	memStr, err := promptWithValidation(events, "RAM (GB)", strconv.Itoa(memGB), validateMemoryGB)
	if err != nil {
		return nil, err
	}
	memGBValue, _ := strconv.Atoi(memStr)
	settings.Memory = memGBValue * 1024

	// Validate final settings
	if err := settings.Validate(); err != nil {
		return nil, fmt.Errorf("invalid settings: %w", err)
	}

	// Save settings
	if err := settings.Save(); err != nil {
		return nil, fmt.Errorf("failed to save settings: %w", err)
	}

	ports.SendEvent(events, ports.UpdateEvent{
		Operation: "Settings",
		Message:   fmt.Sprintf("Saved: IP=%s, Port=%d, RAM=%dGB", settings.IP, settings.Port, settings.Memory/1024),
	})
	ports.SendEvent(events, ports.FinishEvent{Operation: "Settings"})

	return settings, nil
}

// promptWithValidation sends a prompt event and validates the response
// Keeps prompting until valid input is received
func promptWithValidation(events chan<- ports.Event, prompt, defaultValue string, validate func(string) error) (string, error) {
	for {
		responseChan := make(chan any, 1)

		ports.SendEvent(events, ports.PromptEvent{
			ID:           prompt,
			Prompt:       prompt,
			DefaultValue: defaultValue,
			ResponseChan: responseChan,
		})

		rawResponse := <-responseChan
		response, ok := rawResponse.(string)
		if !ok {
			return "", fmt.Errorf("expected string response, got %T", rawResponse)
		}

		if err := validate(response); err != nil {
			ports.SendEvent(events, ports.UpdateEvent{
				Operation: "Settings",
				Message:   fmt.Sprintf("Invalid input: %v", err),
			})
			continue
		}

		return response, nil
	}
}

func validateIP(input string) error {
	if input == "" {
		return fmt.Errorf("IP cannot be empty")
	}
	if net.ParseIP(input) == nil {
		return fmt.Errorf("invalid IP address: %s", input)
	}
	return nil
}

func validatePort(input string) error {
	port, err := strconv.Atoi(input)
	if err != nil {
		return fmt.Errorf("port must be a number")
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func validateMemoryGB(input string) error {
	memoryGB, err := strconv.Atoi(input)
	if err != nil {
		return fmt.Errorf("memory must be a number")
	}
	if memoryGB <= 0 {
		return fmt.Errorf("memory must be positive")
	}
	if memoryGB > 64 {
		return fmt.Errorf("memory cannot exceed 64GB")
	}
	return nil
}
