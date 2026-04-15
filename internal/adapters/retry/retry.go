package retry

import (
	"context"
	"fmt"
	"testing"
	"time"

	rg "github.com/avast/retry-go/v4"
)

var (
	defaultAttempts  uint          = 5
	defaultBaseDelay time.Duration = 1 * time.Second
	defaultMaxDelay  time.Duration = 15 * time.Second
)

func init() {
	if testing.Testing() {
		defaultBaseDelay = 0
		defaultMaxDelay = 0
	}
}

type fatalError struct{ err error }

func (f fatalError) Error() string { return "fatal: " + f.err.Error() }
func (f fatalError) Unwrap() error { return f.err }

// Fatal marks an error as non-retryable and run-abort-worthy (logic error, bug, contract violation).
func Fatal(err error) error { return fatalError{err} }

// IsFatal reports whether err was wrapped via Fatal.
func IsFatal(err error) bool {
	_, ok := err.(fatalError)
	return ok
}

// DefaultOptions returns the shared retry config. Under `go test`, delays are zero.
func DefaultOptions() []rg.Option {
	return []rg.Option{
		rg.Attempts(defaultAttempts),
		rg.Delay(defaultBaseDelay),
		rg.MaxDelay(defaultMaxDelay),
		rg.DelayType(rg.BackOffDelay),
		rg.LastErrorOnly(true),
	}
}

// Do runs fn with default backoff + panic recovery. Panics inside fn become Fatal errors.
// Extra options append to (and can override) DefaultOptions.
func Do[T any](ctx context.Context, fn func(context.Context) (T, error), extra ...rg.Option) (T, error) {
	opts := append(DefaultOptions(), rg.Context(ctx))
	opts = append(opts, extra...)
	return rg.DoWithData(func() (out T, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = Fatal(fmt.Errorf("panic: %v", r))
			}
		}()
		return fn(ctx)
	}, opts...)
}

// DoVoid is the error-only variant of Do.
func DoVoid(ctx context.Context, fn func(context.Context) error, extra ...rg.Option) error {
	_, err := Do(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	}, extra...)
	return err
}
