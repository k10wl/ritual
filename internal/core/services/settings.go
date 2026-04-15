package services

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
)

// PromptSettings loads existing settings and prompts the user via the
// Prompter port for each configurable value. Validation feedback is
// published to the event bus.
//
// minRAMMB is the minimum RAM requirement from the remote manifest (MB).
// Returns validated and saved settings.
func PromptSettings(bus ports.EventBus, prompter ports.Prompter, minRAMMB int) (*domain.Settings, error) {
	if prompter == nil {
		return nil, fmt.Errorf("prompter cannot be nil")
	}
	settings, err := domain.LoadSettings()
	if err != nil {
		return nil, fmt.Errorf("failed to load existing settings: %w", err)
	}

	minRAMGB := minRAMMB / 1024
	if minRAMGB < 1 {
		minRAMGB = 1
	}

	if bus != nil { bus.Publish(ports.StartInfo{Operation: "Settings"}) }
	ports.SendEvent(bus, ports.UpdateInfo{
		Operation: "Settings",
		Message:   "Press Enter to accept default values shown in brackets",
	})

	ctx := context.Background()

	ip, err := promptWithValidation(ctx, bus, prompter, "IP Address", settings.IP, validateIP)
	if err != nil {
		return nil, err
	}
	settings.IP = ip

	portStr, err := promptWithValidation(ctx, bus, prompter, "Port", strconv.Itoa(settings.Port), validatePort)
	if err != nil {
		return nil, err
	}
	settings.Port, _ = strconv.Atoi(portStr)

	memGB := settings.Memory / 1024
	if memGB < minRAMGB {
		memGB = minRAMGB
	}
	memPrompt := fmt.Sprintf("RAM (GB, min %d)", minRAMGB)
	memStr, err := promptWithValidation(ctx, bus, prompter, memPrompt, strconv.Itoa(memGB), makeMemoryValidator(minRAMGB))
	if err != nil {
		return nil, err
	}
	memGBValue, _ := strconv.Atoi(memStr)
	settings.Memory = memGBValue * 1024

	if err := settings.Validate(); err != nil {
		return nil, fmt.Errorf("invalid settings: %w", err)
	}

	if err := settings.Save(); err != nil {
		return nil, fmt.Errorf("failed to save settings: %w", err)
	}

	ports.SendEvent(bus, ports.UpdateInfo{
		Operation: "Settings",
		Message:   fmt.Sprintf("Saved: IP=%s, Port=%d, RAM=%dGB", settings.IP, settings.Port, settings.Memory/1024),
	})
	if bus != nil { bus.Publish(ports.FinishInfo{Operation: "Settings"}) }

	return settings, nil
}

// promptWithValidation asks the user via Prompter until a valid response is given.
func promptWithValidation(ctx context.Context, bus ports.EventBus, prompter ports.Prompter, prompt, defaultValue string, validate func(string) error) (string, error) {
	for {
		response, err := prompter.Prompt(ctx, prompt, prompt, defaultValue)
		if err != nil {
			return "", err
		}
		if err := validate(response); err != nil {
			ports.SendEvent(bus, ports.UpdateInfo{
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

// makeMemoryValidator creates a memory validator with the specified minimum
func makeMemoryValidator(minGB int) func(string) error {
	return func(input string) error {
		memoryGB, err := strconv.Atoi(input)
		if err != nil {
			return fmt.Errorf("memory must be a number")
		}
		if memoryGB < minGB {
			return fmt.Errorf("memory must be at least %dGB (required minimum)", minGB)
		}
		if memoryGB > 64 {
			return fmt.Errorf("memory cannot exceed 64GB")
		}
		return nil
	}
}
