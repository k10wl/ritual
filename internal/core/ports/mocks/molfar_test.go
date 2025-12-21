package mocks

import (
	"ritual/internal/core/domain"
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

	mockMolfar.RunFunc = func(server *domain.Server) error {
		return nil
	}

	server, err := domain.NewServer("127.0.0.1:25565", 1024)
	if err != nil {
		t.Errorf("Failed to create server: %v", err)
	}

	err = molfar.Run(server)
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
