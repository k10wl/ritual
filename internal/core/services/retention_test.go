package services_test

import (
	"context"
	"errors"
	"testing"

	"ritual/internal/core/domain"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/services"
)

func TestRetention_Apply_ListsAndDeletes(t *testing.T) {
	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		if prefix != "backups" {
			t.Errorf("prefix=%s, want backups", prefix)
		}
		return []string{
			"backups/20260414160000/",
			"backups/20260413160000/",
			"backups/20260412160000/",
		}, nil
	}

	deleted := []string{}
	storage.DeleteBatchFunc = func(ctx context.Context, keys []string) error {
		deleted = append(deleted, keys...)
		return nil
	}

	r, err := services.NewRetention(storage, domain.RetentionRules{KeepLast: 2}, "backups", services.ParseTimestampDir)
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(deleted) != 1 || deleted[0] != "backups/20260412160000/" {
		t.Errorf("deleted=%v, want [backups/20260412160000/]", deleted)
	}
}

func TestRetention_Apply_Empty_NoOp(t *testing.T) {
	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, nil
	}
	storage.DeleteBatchFunc = func(ctx context.Context, keys []string) error {
		t.Errorf("DeleteBatch called unexpectedly: %v", keys)
		return nil
	}

	r, _ := services.NewRetention(storage, domain.RetentionRules{KeepLast: 2}, "backups", services.ParseTimestampDir)
	if err := r.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRetention_Apply_ListError_Propagates(t *testing.T) {
	storage := &mocks.MockStorageRepository{}
	want := errors.New("list boom")
	storage.ListFunc = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, want
	}

	r, _ := services.NewRetention(storage, domain.RetentionRules{KeepLast: 2}, "backups", services.ParseTimestampDir)
	err := r.Apply(context.Background())
	if !errors.Is(err, want) {
		t.Errorf("got %v, want %v wrapped", err, want)
	}
}

func TestRetention_NewRetention_NilStorage_Errors(t *testing.T) {
	_, err := services.NewRetention(nil, domain.RetentionRules{}, "backups", services.ParseTimestampDir)
	if err == nil {
		t.Error("expected error for nil storage")
	}
}
