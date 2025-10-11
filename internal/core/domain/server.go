package domain

import (
	"fmt"
	"net"
	"strconv"
)

const (
	DefaultBatPath = "server.bat"
)

// Server represents a Minecraft server configuration
type Server struct {
	Address string `json:"address"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
	Memory  int    `json:"memory"`
	BatPath string `json:"bat_path"`
}

// NewServer creates a new Server instance with address parsing
func NewServer(address string, memory int) (*Server, error) {
	if address == "" {
		return nil, fmt.Errorf("address cannot be empty")
	}
	if memory <= 0 {
		return nil, fmt.Errorf("memory must be positive")
	}

	ip, port, err := parseAddress(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address format: %w", err)
	}

	server := &Server{
		Address: address,
		IP:      ip,
		Port:    port,
		Memory:  memory,
		BatPath: DefaultBatPath,
	}

	if server.IP == "" {
		return nil, fmt.Errorf("parsed IP cannot be empty")
	}
	if server.Port <= 0 {
		return nil, fmt.Errorf("parsed port must be positive")
	}

	return server, nil
}

// parseAddress extracts IP and port from address string
func parseAddress(address string) (string, int, error) {
	if address == "" {
		return "", 0, fmt.Errorf("address cannot be empty")
	}

	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, fmt.Errorf("failed to split host and port: %w", err)
	}

	if host == "" {
		return "", 0, fmt.Errorf("host cannot be empty")
	}
	if portStr == "" {
		return "", 0, fmt.Errorf("port cannot be empty")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port format: %w", err)
	}

	if port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("port must be between 1 and 65535")
	}

	return host, port, nil
}
