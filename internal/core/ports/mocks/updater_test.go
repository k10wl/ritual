package mocks

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockUpdaterService_Run_Success(t *testing.T) {
	mock := NewMockUpdaterService()
	ctx := context.Background()

	err := mock.Run(ctx)

	assert.NoError(t, err)
	assert.True(t, mock.RunCalled)
	assert.Equal(t, 1, mock.RunCount)
}

func TestMockUpdaterService_Run_MultipleCalls(t *testing.T) {
	mock := NewMockUpdaterService()
	ctx := context.Background()

	_ = mock.Run(ctx)
	_ = mock.Run(ctx)
	_ = mock.Run(ctx)

	assert.True(t, mock.RunCalled)
	assert.Equal(t, 3, mock.RunCount)
}

func TestMockUpdaterService_Run_WithFunction(t *testing.T) {
	mock := NewMockUpdaterService()
	ctx := context.Background()
	expectedError := errors.New("update failed")

	mock.RunFunc = func(ctx context.Context) error {
		return expectedError
	}

	err := mock.Run(ctx)

	assert.Equal(t, expectedError, err)
	assert.True(t, mock.RunCalled)
}

func TestMockUpdaterService_Run_NilContext(t *testing.T) {
	mock := NewMockUpdaterService()

	err := mock.Run(nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context cannot be nil")
}

func TestMockUpdaterService_Run_NilMock(t *testing.T) {
	var mock *MockUpdaterService
	ctx := context.Background()

	err := mock.Run(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mock updater service cannot be nil")
}

func TestMockUpdaterService_Reset(t *testing.T) {
	mock := NewMockUpdaterService()
	ctx := context.Background()

	mock.RunFunc = func(ctx context.Context) error {
		return nil
	}
	_ = mock.Run(ctx)

	assert.True(t, mock.RunCalled)
	assert.Equal(t, 1, mock.RunCount)

	mock.Reset()

	assert.False(t, mock.RunCalled)
	assert.Equal(t, 0, mock.RunCount)
	assert.Nil(t, mock.RunFunc)
}

func TestMockUpdaterService_Reset_Nil(t *testing.T) {
	var mock *MockUpdaterService

	// Should not panic
	mock.Reset()
}

func TestMockUpdaterService_ImplementsInterface(t *testing.T) {
	mock := NewMockUpdaterService()

	// Verify mock implements the interface by calling methods
	ctx := context.Background()
	err := mock.Run(ctx)
	assert.NoError(t, err)
}
