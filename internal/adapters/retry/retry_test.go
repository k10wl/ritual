package retry_test

import (
	"context"
	"errors"
	"testing"

	rg "github.com/avast/retry-go/v4"

	"ritual/internal/adapters/retry"
)

func TestDo_SucceedsFirstTry(t *testing.T) {
	n := 0
	got, err := retry.Do(context.Background(), func(ctx context.Context) (int, error) {
		n++
		return 42, nil
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != 42 {
		t.Errorf("got = %d, want 42", got)
	}
	if n != 1 {
		t.Errorf("attempts = %d, want 1", n)
	}
}

func TestDo_RetriesUntilSuccess(t *testing.T) {
	n := 0
	got, err := retry.Do(context.Background(), func(ctx context.Context) (int, error) {
		n++
		if n < 3 {
			return 0, errors.New("flaky")
		}
		return 7, nil
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != 7 {
		t.Errorf("got = %d, want 7", got)
	}
	if n != 3 {
		t.Errorf("attempts = %d, want 3", n)
	}
}

func TestDo_ExhaustsAttempts(t *testing.T) {
	n := 0
	_, err := retry.Do(context.Background(), func(ctx context.Context) (int, error) {
		n++
		return 0, errors.New("boom")
	}, rg.Attempts(3))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if n != 3 {
		t.Errorf("attempts = %d, want 3", n)
	}
}

func TestDo_PanicBecomesFatal(t *testing.T) {
	_, err := retry.Do(context.Background(), func(ctx context.Context) (int, error) {
		panic("kaboom")
	}, rg.Attempts(5))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !retry.IsFatal(err) {
		t.Errorf("want Fatal, got %v", err)
	}
}

func TestDo_PanicShapes(t *testing.T) {
	cases := []struct {
		name string
		val  any
	}{
		{"string", "str"},
		{"error", errors.New("err")},
		{"int", 42},
		{"nil", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := retry.Do(context.Background(), func(ctx context.Context) (int, error) {
				if tc.val == nil {
					panic(tc.val)
				}
				panic(tc.val)
			}, rg.Attempts(1))
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !retry.IsFatal(err) {
				t.Errorf("want Fatal, got %v", err)
			}
		})
	}
}

func TestDo_ClassifierSkipsRetry(t *testing.T) {
	n := 0
	_, err := retry.Do(context.Background(), func(ctx context.Context) (int, error) {
		n++
		return 0, errors.New("permanent")
	}, rg.RetryIf(func(error) bool { return false }), rg.Attempts(5))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if n != 1 {
		t.Errorf("attempts = %d, want 1 (classifier said no retry)", n)
	}
}

func TestDo_CtxCancelStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := 0
	_, err := retry.Do(ctx, func(ctx context.Context) (int, error) {
		n++
		return 0, errors.New("x")
	}, rg.Attempts(10))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if n > 1 {
		t.Errorf("attempts = %d, want <= 1 (ctx already cancelled)", n)
	}
}

func TestDo_OnRetryFiresPerFailure(t *testing.T) {
	var hookCalls int
	n := 0
	_, err := retry.Do(context.Background(), func(ctx context.Context) (int, error) {
		n++
		if n < 3 {
			return 0, errors.New("flaky")
		}
		return 1, nil
	}, rg.OnRetry(func(_ uint, _ error) { hookCalls++ }))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if hookCalls != 2 {
		t.Errorf("hook calls = %d, want 2", hookCalls)
	}
}

func TestDoVoid_RetriesUntilSuccess(t *testing.T) {
	n := 0
	err := retry.DoVoid(context.Background(), func(ctx context.Context) error {
		n++
		if n < 2 {
			return errors.New("flaky")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if n != 2 {
		t.Errorf("attempts = %d, want 2", n)
	}
}

func TestFatal_UnwrapsInner(t *testing.T) {
	inner := errors.New("root")
	wrapped := retry.Fatal(inner)
	if !retry.IsFatal(wrapped) {
		t.Fatal("IsFatal = false, want true")
	}
	if !errors.Is(wrapped, inner) {
		t.Fatal("errors.Is fails — Unwrap broken")
	}
}

func TestIsFatal_PlainError(t *testing.T) {
	if retry.IsFatal(errors.New("regular")) {
		t.Fatal("IsFatal reported true for non-Fatal error")
	}
	if retry.IsFatal(nil) {
		t.Fatal("IsFatal(nil) = true")
	}
}
