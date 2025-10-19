package mocks

import (
	"errors"
	"testing"
)

func TestMockBackupperService_Run_Success(t *testing.T) {
	mock := NewMockBackupperService().(*MockBackupperService)

	cleanup, err := mock.Run()
	if err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
	if cleanup == nil {
		t.Error("Run() cleanup function is nil")
	}
}

func TestMockBackupperService_Run_WithFunction(t *testing.T) {
	mock := NewMockBackupperService().(*MockBackupperService)
	expectedError := errors.New("backup failed")

	mock.RunFunc = func() (func() error, error) {
		return func() error { return nil }, expectedError
	}

	cleanup, err := mock.Run()
	if err != expectedError {
		t.Errorf("Run() error = %v, want %v", err, expectedError)
	}
	if cleanup == nil {
		t.Error("Run() cleanup function is nil")
	}
}

func TestMockBackupperService_Exit_Success(t *testing.T) {
	mock := NewMockBackupperService().(*MockBackupperService)

	err := mock.Exit()
	if err != nil {
		t.Errorf("Exit() error = %v, want nil", err)
	}
}
