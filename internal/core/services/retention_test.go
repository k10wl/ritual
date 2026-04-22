package services_test

import (
	"context"
	"errors"
	"ritual/internal/core/domain"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/services"
	"testing"
	"time"
)

func TestRefsRetention_Select_ListsRefsAndReturnsMarkedDrops(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(_ context.Context, prefix string) ([]string, error) {
		if prefix != "refs/" {
			t.Errorf("refs retention must list the refs/ keyspace, got prefix %q", prefix)
		}
		return []string{
			"refs/20260414160000.json",
			"refs/20260413160000.json",
			"refs/20260412160000.json",
		}, nil
	}

	r := services.NewRefsRetention(storage, domain.RetentionRules{KeepLast: 2})
	got, err := r.Select(ctx)

	if err != nil {
		t.Fatalf("healthy list must not error: %v", err)
	}
	if len(got) != 1 || got[0] != "refs/20260412160000.json" {
		t.Errorf("KeepLast:2 must drop only the oldest; got %v", got)
	}
}

func TestRefsRetention_Select_ListError_Propagates(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	boom := errors.New("list boom")
	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(_ context.Context, _ string) ([]string, error) {
		return nil, boom
	}

	r := services.NewRefsRetention(storage, domain.RetentionRules{KeepLast: 2})
	_, err := r.Select(ctx)

	if !errors.Is(err, boom) {
		t.Errorf("List error must rise to Select caller; got %v", err)
	}
}

func TestRefsRetention_Select_EmptyList_NoDrops(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(_ context.Context, _ string) ([]string, error) { return nil, nil }

	r := services.NewRefsRetention(storage, domain.RetentionRules{KeepLast: 2})
	got, err := r.Select(ctx)

	if err != nil {
		t.Fatalf("empty list must not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty input must yield no drops; got %v", got)
	}
}

func TestLogsRetention_Select_ListsLogsAndTrimsByKeepLast(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(_ context.Context, prefix string) ([]string, error) {
		if prefix != "logs" {
			t.Errorf("logs retention must list the logs keyspace, got prefix %q", prefix)
		}
		return []string{
			"logs/20260414160000.log",
			"logs/20260413160000.log",
			"logs/20260412160000.log",
			"logs/20260411160000.log",
		}, nil
	}

	r := services.NewLogsRetention(storage, domain.RetentionRules{KeepLast: 2})
	got, err := r.Select(ctx)

	if err != nil {
		t.Fatalf("healthy list must not error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("KeepLast:2 across 4 logs must drop 2; got %v", got)
	}
}

func TestLogsRetention_Select_UnknownFile_IsPreserved(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	storage := &mocks.MockStorageRepository{}
	storage.ListFunc = func(_ context.Context, _ string) ([]string, error) {
		return []string{
			"logs/20260414160000.log",
			"logs/latest.log",
		}, nil
	}

	r := services.NewLogsRetention(storage, domain.RetentionRules{KeepLast: 1})
	got, err := r.Select(ctx)

	if err != nil {
		t.Fatalf("healthy list must not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("non-timestamp filename must be left alone; got %v", got)
	}
}
