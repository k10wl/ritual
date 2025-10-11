package mocks

import (
	"ritual/internal/core/ports"
	"testing"
)

func TestMockMolfarService(t *testing.T) {
	mock := NewMockMolfarService()

	var molfar ports.MolfarService = mock
	if molfar == nil {
		t.Error("MockMolfarService does not implement MolfarService interface")
	}

	mockMolfar := mock.(*MockMolfarService)
	mockMolfar.PrepareFunc = func() error {
		return nil
	}

	err := molfar.Prepare()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mockMolfar.RunFunc = func() error {
		return nil
	}

	err = molfar.Run()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	mockMolfar.ExitFunc = func() error {
		return nil
	}

	err = molfar.Exit()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}
