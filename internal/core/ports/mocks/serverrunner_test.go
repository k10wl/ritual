package mocks

import (
	"errors"
	"ritual/internal/core/ports"
	"testing"
)

func TestMockServerRunner_ImplementsInterface(t *testing.T) {
	var _ ports.ServerRunner = NewMockServerRunner()
}

func TestMockServerRunner_Run(t *testing.T) {
	mock := NewMockServerRunner()

	err := mock.Run()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestMockServerRunner_RunWithError(t *testing.T) {
	mockRunner, ok := NewMockServerRunner().(*MockServerRunner)
	if !ok {
		t.Fatalf("Failed to cast NewMockServerRunner() to *MockServerRunner")
	}
	expectedErr := errors.New("test error")
	mockRunner.RunFunc = func() error {
		return expectedErr
	}

	err := mockRunner.Run()
	if err != expectedErr {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}
}
