package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewServerRuntime(t *testing.T) {
	tests := []struct {
		name      string
		port      int
		memory    int
		wantError bool
	}{
		{
			name:      "valid server",
			port:      25565,
			memory:    1024,
			wantError: false,
		},
		{
			name:      "zero port",
			port:      0,
			memory:    1024,
			wantError: true,
		},
		{
			name:      "negative port",
			port:      -1,
			memory:    1024,
			wantError: true,
		},
		{
			name:      "port too high",
			port:      99999,
			memory:    1024,
			wantError: true,
		},
		{
			name:      "zero memory",
			port:      25565,
			memory:    0,
			wantError: true,
		},
		{
			name:      "negative memory",
			port:      25565,
			memory:    -1,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewServerRuntime(tt.port, tt.memory)

			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, server)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, server)
				assert.Equal(t, tt.port, server.Port)
				assert.Equal(t, tt.memory, server.Memory)
			}
		})
	}
}
