package domain

import "errors"

// ServerRuntime represents a Minecraft server configuration
type ServerRuntime struct {
	Port   int `json:"port"`
	Memory int `json:"memory"`
}

// NewServerRuntime creates a new ServerRuntime instance
func NewServerRuntime(port, memory int) (*ServerRuntime, error) {
	if port <= 0 || port > 65535 {
		return nil, errors.New("port must be between 1 and 65535")
	}
	if memory <= 0 {
		return nil, errors.New("memory must be positive")
	}

	return &ServerRuntime{
		Port:   port,
		Memory: memory,
	}, nil
}
