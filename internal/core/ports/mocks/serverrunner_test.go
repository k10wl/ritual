package mocks

import (
	"errors"
	"ritual/internal/core/ports"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockServerRunner_ImplementsInterface(t *testing.T) {
	var _ ports.ServerRunner = NewMockServerRunner()
}

func TestMockServerRunner_Run(t *testing.T) {
	mock := NewMockServerRunner()

	err := mock.Run()
	assert.NoError(t, err)
}

func TestMockServerRunner_RunWithError(t *testing.T) {
	mockRunner, ok := NewMockServerRunner().(*MockServerRunner)
	assert.True(t, ok, "Failed to cast NewMockServerRunner() to *MockServerRunner")

	expectedErr := errors.New("test error")
	mockRunner.RunFunc = func() error {
		return expectedErr
	}

	err := mockRunner.Run()
	assert.Equal(t, expectedErr, err)
}
