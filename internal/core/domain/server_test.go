package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewServer(t *testing.T) {
	tests := []struct {
		name      string
		address   string
		memory    int
		wantError bool
	}{
		{
			name:      "valid server",
			address:   "127.0.0.1:25565",
			memory:    1024,
			wantError: false,
		},
		{
			name:      "empty address",
			address:   "",
			memory:    1024,
			wantError: true,
		},
		{
			name:      "zero memory",
			address:   "127.0.0.1:25565",
			memory:    0,
			wantError: true,
		},
		{
			name:      "negative memory",
			address:   "127.0.0.1:25565",
			memory:    -1,
			wantError: true,
		},
		{
			name:      "invalid address format",
			address:   "invalid",
			memory:    1024,
			wantError: true,
		},
		{
			name:      "invalid port",
			address:   "127.0.0.1:99999",
			memory:    1024,
			wantError: true,
		},
		{
			name:      "hostname instead of IP",
			address:   "localhost:25565",
			memory:    1024,
			wantError: true,
		},
		{
			name:      "domain name instead of IP",
			address:   "example.com:25565",
			memory:    1024,
			wantError: true,
		},
		{
			name:      "IPv6 address",
			address:   "[::1]:25565",
			memory:    1024,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewServer(tt.address, tt.memory)

			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, server)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, server)
				assert.Equal(t, tt.address, server.Address)
				assert.Equal(t, tt.memory, server.Memory)
				assert.Equal(t, DefaultBatPath, server.BatPath)
			}
		})
	}
}

func TestDefaultBatPath(t *testing.T) {
	assert.Equal(t, "server.bat", DefaultBatPath)
}
