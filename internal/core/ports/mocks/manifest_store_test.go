package mocks

import (
	"context"
	"errors"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports"
	"testing"
)

func TestMockManifestStore_ImplementsPort(t *testing.T) {
	var _ ports.ManifestStore = (*MockManifestStore)(nil)
}

func TestMockManifestStore_DefaultNoOp(t *testing.T) {
	m := &MockManifestStore{}

	got, err := m.Get(context.Background())
	if err != nil {
		t.Fatalf("default Get err = %v", err)
	}
	if got != nil {
		t.Errorf("default Get returned %+v, want nil", got)
	}
	if err := m.Save(context.Background(), nil); err != nil {
		t.Fatalf("default Save err = %v", err)
	}
	if m.GetCalls != 1 || m.SaveCalls != 1 {
		t.Errorf("call counts: Get=%d Save=%d", m.GetCalls, m.SaveCalls)
	}
}

func TestMockManifestStore_FuncsInvoked(t *testing.T) {
	want := &domain.Manifest{ManifestVersion: "1.0"}
	boom := errors.New("boom")

	m := &MockManifestStore{
		GetFunc:  func(ctx context.Context) (*domain.Manifest, error) { return want, nil },
		SaveFunc: func(ctx context.Context, mf *domain.Manifest) error { return boom },
	}

	got, err := m.Get(context.Background())
	if err != nil || got != want {
		t.Errorf("Get = (%v, %v), want (%v, nil)", got, err, want)
	}

	if err := m.Save(context.Background(), want); !errors.Is(err, boom) {
		t.Errorf("Save err = %v, want boom", err)
	}

	if m.GetCalls != 1 || m.SaveCalls != 1 {
		t.Errorf("call counts: Get=%d Save=%d", m.GetCalls, m.SaveCalls)
	}
}
