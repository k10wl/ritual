package mocks

import (
	"testing"

	"ritual/internal/core/domain"

	"github.com/stretchr/testify/assert"
)

func TestMockServerRunner_Run(t *testing.T) {
	mockRunner := NewMockServerRunner()
	server := &domain.Server{Address: "127.0.0.1:25565", IP: "127.0.0.1", Port: 25565, Memory: 1024}

	mockRunner.On("Run", server).Return(nil)

	err := mockRunner.Run(server)
	assert.NoError(t, err)

	mockRunner.AssertExpectations(t)
}

func TestMockServerRunner_Run_Error(t *testing.T) {
	mockRunner := NewMockServerRunner()
	server := &domain.Server{Address: "127.0.0.1:25565", IP: "127.0.0.1", Port: 25565, Memory: 1024}

	expectedError := assert.AnError
	mockRunner.On("Run", server).Return(expectedError)

	err := mockRunner.Run(server)
	assert.Error(t, err)
	assert.Equal(t, expectedError, err)

	mockRunner.AssertExpectations(t)
}
