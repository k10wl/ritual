package retaining_test

import (
	"context"
	"errors"
	"ritual/internal/core/ports/mocks"
	"ritual/internal/core/stages/retaining"
	"testing"
	"time"
)

type retentionFunc func(ctx context.Context) ([]string, error)

func (f retentionFunc) Select(ctx context.Context) ([]string, error) { return f(ctx) }

type collectorFunc func(ctx context.Context) error

func (f collectorFunc) Collect(ctx context.Context) error { return f(ctx) }

func TestRefsJob_HealthyPath_DeletesSelectedKeysThenCollects(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	ret := retentionFunc(func(_ context.Context) ([]string, error) {
		return []string{"refs/20260414160000.json"}, nil
	})

	var deleted []string
	storage := &mocks.MockStorageRepository{}
	storage.DeleteBatchFunc = func(_ context.Context, keys []string) error {
		deleted = append(deleted, keys...)
		return nil
	}

	collectCalled := false
	coll := collectorFunc(func(_ context.Context) error { collectCalled = true; return nil })

	job := retaining.NewRefsJob(ret, storage, coll)
	err := job(ctx)

	if err != nil {
		t.Fatalf("healthy run must not error: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "refs/20260414160000.json" {
		t.Errorf("selected keys must be batch-deleted; got %v", deleted)
	}
	if !collectCalled {
		t.Errorf("collector must always run after refs retention")
	}
}

func TestRefsJob_EmptySelection_SkipsDelete_StillCollects(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	ret := retentionFunc(func(_ context.Context) ([]string, error) { return nil, nil })

	deleteCalled := false
	storage := &mocks.MockStorageRepository{}
	storage.DeleteBatchFunc = func(_ context.Context, _ []string) error {
		deleteCalled = true
		return nil
	}

	collectCalled := false
	coll := collectorFunc(func(_ context.Context) error { collectCalled = true; return nil })

	err := retaining.NewRefsJob(ret, storage, coll)(ctx)

	if err != nil {
		t.Fatalf("empty selection must not error: %v", err)
	}
	if deleteCalled {
		t.Errorf("empty selection must not invoke DeleteBatch")
	}
	if !collectCalled {
		t.Errorf("collector is independent of selection — must run even when nothing to delete")
	}
}

func TestRefsJob_DeleteFails_CollectStillRuns_ErrorsJoined(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	ret := retentionFunc(func(_ context.Context) ([]string, error) {
		return []string{"refs/20260414160000.json"}, nil
	})

	storage := &mocks.MockStorageRepository{}
	storage.DeleteBatchFunc = func(_ context.Context, _ []string) error {
		return errors.New("delete-boom")
	}

	collectCalled := false
	coll := collectorFunc(func(_ context.Context) error { collectCalled = true; return nil })

	err := retaining.NewRefsJob(ret, storage, coll)(ctx)

	if err == nil || !errorContains(err, "delete-boom") {
		t.Errorf("delete error must surface via errors.Join; got %v", err)
	}
	if !collectCalled {
		t.Errorf("collect must run even after delete failure — GC is independent")
	}
}

func TestRefsJob_SelectFails_NoDelete_CollectStillRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	ret := retentionFunc(func(_ context.Context) ([]string, error) {
		return nil, errors.New("select-boom")
	})

	deleteCalled := false
	storage := &mocks.MockStorageRepository{}
	storage.DeleteBatchFunc = func(_ context.Context, _ []string) error {
		deleteCalled = true
		return nil
	}

	collectCalled := false
	coll := collectorFunc(func(_ context.Context) error { collectCalled = true; return nil })

	err := retaining.NewRefsJob(ret, storage, coll)(ctx)

	if err == nil || !errorContains(err, "select-boom") {
		t.Errorf("select error must surface; got %v", err)
	}
	if deleteCalled {
		t.Errorf("select failure must not invoke DeleteBatch — we have no keys to act on")
	}
	if !collectCalled {
		t.Errorf("collect must run even after select failure — GC is independent")
	}
}

func TestLogsJob_HealthyPath_DeletesSelectedKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	ret := retentionFunc(func(_ context.Context) ([]string, error) {
		return []string{"logs/20260414160000.log"}, nil
	})

	var deleted []string
	storage := &mocks.MockStorageRepository{}
	storage.DeleteBatchFunc = func(_ context.Context, keys []string) error {
		deleted = append(deleted, keys...)
		return nil
	}

	err := retaining.NewLogsJob(ret, storage)(ctx)

	if err != nil {
		t.Fatalf("healthy run must not error: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "logs/20260414160000.log" {
		t.Errorf("selected log keys must be batch-deleted; got %v", deleted)
	}
}

func TestLogsJob_EmptySelection_SkipsDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	ret := retentionFunc(func(_ context.Context) ([]string, error) { return nil, nil })

	deleteCalled := false
	storage := &mocks.MockStorageRepository{}
	storage.DeleteBatchFunc = func(_ context.Context, _ []string) error {
		deleteCalled = true
		return nil
	}

	err := retaining.NewLogsJob(ret, storage)(ctx)

	if err != nil {
		t.Fatalf("empty selection must not error: %v", err)
	}
	if deleteCalled {
		t.Errorf("empty selection must not invoke DeleteBatch")
	}
}

func errorContains(err error, substr string) bool {
	return err != nil && (err.Error() == substr || contains(err.Error(), substr))
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
