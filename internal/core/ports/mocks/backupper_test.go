package mocks

import (
	"context"
	"errors"
	"testing"
)

func TestMockBackupperService_Run_Success(t *testing.T) {
	mock := NewMockBackupperService().(*MockBackupperService)
	ctx := context.Background()

	_, err := mock.Run(ctx)
	if err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
}

func TestMockBackupperService_Run_WithFunction(t *testing.T) {
	mock := NewMockBackupperService().(*MockBackupperService)
	ctx := context.Background()
	expectedError := errors.New("backup failed")

	mock.RunFunc = func(ctx context.Context) (string, error) {
		return "", expectedError
	}

	_, err := mock.Run(ctx)
	if err != expectedError {
		t.Errorf("Run() error = %v, want %v", err, expectedError)
	}
}
