// Package stream provides retry-on-transient-error primitives for byte
// streams. Resumable wraps a re-openable source so that mid-stream blips
// (R2 body EOFs, network timeouts) heal transparently without surfacing
// to the io.Reader caller.
package stream

import (
	"context"
	"errors"
	"io"
	"time"
)

// RetryPolicy parameterises both stream.Resumable and the call-level retry
// loop in adapters.RetryingStorage. Classify decides whether an error is
// worth retrying; Sleep blocks (respecting ctx) for the supplied backoff and
// returns ctx.Err on cancellation; OnRetry — optional — is invoked before
// each retry attempt for observability.
type RetryPolicy struct {
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	Classify    func(error) bool
	Sleep       func(ctx context.Context, d time.Duration) error
	OnRetry     func(attempt int, offset int64, err error)
}

// Opener re-opens the source at the supplied byte offset. offset=0 means
// "from the start". Implementations that have native range support open a
// partial response; range-blind sources re-open at 0 and skip bytes
// internally before returning the ReadCloser.
type Opener func(ctx context.Context, offset int64) (io.ReadCloser, error)

// Resumable presents a single, continuous io.ReadCloser over a sequence of
// underlying bodies. On a classified transient Read error the current body
// is closed and the Opener is re-invoked at the current byte offset; the
// caller sees an uninterrupted byte stream. Non-transient errors and io.EOF
// pass through unchanged. Concurrent Read on one Resumable is undefined —
// the same constraint as io.Reader itself.
type Resumable struct {
	open    Opener
	policy  RetryPolicy
	ctx     context.Context
	body    io.ReadCloser
	offset  int64
	attempt int
	lastErr error
	closed  bool
}

// NewResumable builds a Resumable rooted at offset 0. The initial open is
// performed eagerly so callers see the same failure semantics as a plain
// GetStream when the source is missing — Resumable is only interesting once
// the first body is in hand.
func NewResumable(ctx context.Context, policy RetryPolicy, open Opener) (*Resumable, error) {
	body, err := open(ctx, 0)
	if err != nil {
		return nil, err
	}
	return &Resumable{open: open, policy: policy, ctx: ctx, body: body}, nil
}

// Read delivers bytes from the underlying body, transparently swapping to a
// fresh body on classified transient errors. If a Read returns (n>0, err
// transient), the n bytes are returned with a nil error and the retry is
// deferred to the next Read — caller never loses progress.
func (r *Resumable) Read(p []byte) (int, error) {
	for {
		if r.body == nil {
			if err := r.reopen(); err != nil {
				return 0, err
			}
		}
		n, err := r.body.Read(p)
		r.offset += int64(n)
		if err == nil || errors.Is(err, io.EOF) {
			return n, err
		}
		if !r.policy.Classify(err) {
			return n, err
		}
		_ = r.body.Close()
		r.body = nil
		r.lastErr = err
		if n > 0 {
			return n, nil
		}
	}
}

// reopen consumes one retry budget unit, sleeps the next backoff, and
// re-invokes the Opener at the current offset.
func (r *Resumable) reopen() error {
	if r.attempt >= r.policy.MaxAttempts-1 {
		return r.lastErr
	}
	r.attempt++
	if r.policy.OnRetry != nil {
		r.policy.OnRetry(r.attempt, r.offset, r.lastErr)
	}
	if err := r.policy.Sleep(r.ctx, backoffFor(r.policy, r.attempt)); err != nil {
		return err
	}
	body, err := r.open(r.ctx, r.offset)
	if err != nil {
		return err
	}
	r.body = body
	return nil
}

// Close releases the current body if one is held. Idempotent.
func (r *Resumable) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.body == nil {
		return nil
	}
	return r.body.Close()
}

// backoffFor returns the backoff for the n-th attempt (n ≥ 1) using exponential
// growth capped at MaxBackoff. Pure function so policies stay testable.
func backoffFor(p RetryPolicy, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := p.BaseBackoff << (attempt - 1)
	if d <= 0 || d > p.MaxBackoff {
		return p.MaxBackoff
	}
	return d
}
