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

func TestRetentionRefsJob_HealthyPath_DeletesSelectedKeys(t *testing.T) {
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

	job := retaining.NewRetentionRefsJob("refs-local", ret, storage)
	if job.Kind != retaining.KindRetention {
		t.Fatalf("retention refs job must carry KindRetention; got %v", job.Kind)
	}
	if job.Label != "refs-local" {
		t.Fatalf("label must round-trip from constructor; got %q", job.Label)
	}

	if err := job.Run(ctx); err != nil {
		t.Fatalf("healthy run must not error: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "refs/20260414160000.json" {
		t.Errorf("selected keys must be batch-deleted; got %v", deleted)
	}
}

func TestRetentionRefsJob_EmptySelection_SkipsDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	ret := retentionFunc(func(_ context.Context) ([]string, error) { return nil, nil })

	deleteCalled := false
	storage := &mocks.MockStorageRepository{}
	storage.DeleteBatchFunc = func(_ context.Context, _ []string) error {
		deleteCalled = true
		return nil
	}

	if err := retaining.NewRetentionRefsJob("refs-local", ret, storage).Run(ctx); err != nil {
		t.Fatalf("empty selection must not error: %v", err)
	}
	if deleteCalled {
		t.Errorf("empty selection must not invoke DeleteBatch")
	}
}

func TestRetentionRefsJob_DeleteFails_SurfacesWrappedError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	ret := retentionFunc(func(_ context.Context) ([]string, error) {
		return []string{"refs/20260414160000.json"}, nil
	})

	storage := &mocks.MockStorageRepository{}
	storage.DeleteBatchFunc = func(_ context.Context, _ []string) error {
		return errors.New("delete-boom")
	}

	err := retaining.NewRetentionRefsJob("refs-local", ret, storage).Run(ctx)
	if err == nil || !errorContains(err, "delete-boom") {
		t.Errorf("delete error must surface wrapped; got %v", err)
	}
}

func TestRetentionRefsJob_SelectFails_NoDelete(t *testing.T) {
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

	err := retaining.NewRetentionRefsJob("refs-local", ret, storage).Run(ctx)
	if err == nil || !errorContains(err, "select-boom") {
		t.Errorf("select error must surface; got %v", err)
	}
	if deleteCalled {
		t.Errorf("select failure must not invoke DeleteBatch — we have no keys to act on")
	}
}

func TestGCRefsJob_HealthyPath_RunsCollector(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	collectCalled := false
	coll := collectorFunc(func(_ context.Context) error { collectCalled = true; return nil })

	job := retaining.NewGCRefsJob("gc-refs-local", coll)
	if job.Kind != retaining.KindGC {
		t.Fatalf("gc refs job must carry KindGC; got %v", job.Kind)
	}
	if job.Label != "gc-refs-local" {
		t.Fatalf("label must round-trip from constructor; got %q", job.Label)
	}

	if err := job.Run(ctx); err != nil {
		t.Fatalf("healthy run must not error: %v", err)
	}
	if !collectCalled {
		t.Errorf("collector must be invoked")
	}
}

func TestGCRefsJob_CollectorFails_SurfacesWrappedError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	coll := collectorFunc(func(_ context.Context) error { return errors.New("collect-boom") })

	err := retaining.NewGCRefsJob("gc-refs-local", coll).Run(ctx)
	if err == nil || !errorContains(err, "collect-boom") {
		t.Errorf("collect error must surface wrapped; got %v", err)
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

	job := retaining.NewLogsJob("logs-local", ret, storage)
	if job.Kind != retaining.KindRetention {
		t.Fatalf("logs job is retention-only; got Kind=%v", job.Kind)
	}

	if err := job.Run(ctx); err != nil {
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

	if err := retaining.NewLogsJob("logs-local", ret, storage).Run(ctx); err != nil {
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
