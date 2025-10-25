package mocks

import (
	"errors"
	"testing"
)

func TestMockBackupperService_Run_Success(t *testing.T) {
	mock := NewMockBackupperService().(*MockBackupperService)

	err := mock.Run()
	if err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
}

func TestMockBackupperService_Run_WithFunction(t *testing.T) {
	mock := NewMockBackupperService().(*MockBackupperService)
	expectedError := errors.New("backup failed")

	mock.RunFunc = func() error {
		return expectedError
	}

	err := mock.Run()
	if err != expectedError {
		t.Errorf("Run() error = %v, want %v", err, expectedError)
	}
}
