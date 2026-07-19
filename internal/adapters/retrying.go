package adapters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"ritual/internal/adapters/stream"
	"ritual/internal/core/ports"
	"syscall"
	"time"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// RangeGetter is the optional capability satisfied by storage adapters that
// can serve a partial body from a byte offset. RetryingStorage type-asserts
// it on its inner; adapters that satisfy it (R2 via HTTP Range) get
// zero-cost mid-stream resume. Adapters that don't pay re-open + skip the
// already-read prefix. Capability lives next to the decorator — port surface
// stays unchanged (feedback_no_interface_bloat.md).
type RangeGetter interface {
	GetStreamRange(ctx context.Context, key string, offset int64) (io.ReadCloser, error)
}

// RetryingStorage decorates a StorageRepository with classified retry on
// transient errors. GetStream returns a stream.Resumable that heals
// mid-stream blips at the byte-stream layer; PutStream retries seekable
// bodies between attempts (non-seekable bodies upload once, surfacing the
// first error honestly); call-level verbs (Exists, List, Copy, Delete*)
// retry per call with the same policy.
type RetryingStorage struct {
	inner  ports.StorageRepository
	policy stream.RetryPolicy
	bus    ports.EventBus
}

// NewRetryingStorage builds a decorator around inner. The bus is optional;
// nil silently disables retry-attempt observability.
func NewRetryingStorage(inner ports.StorageRepository, policy stream.RetryPolicy, bus ports.EventBus) *RetryingStorage {
	return &RetryingStorage{inner: inner, policy: policy, bus: bus}
}

// String labels the decorator as "retrying::<inner>" so observability events
// pin retry attribution to this layer specifically.
func (r *RetryingStorage) String() string { return "retrying::" + fmt.Sprint(r.inner) }

// GetStream returns a Resumable wrapping the inner body. The Opener uses
// inner's RangeGetter capability when available; otherwise it falls back to
// re-open + skip-prefix. Initial open and mid-stream resume each own their
// own retry budget: the initial open runs through runWithRetry (transient
// open-time errors survive the SDK's request-phase retry only to land here),
// then the returned Resumable handles mid-stream blips on its own budget.
func (r *RetryingStorage) GetStream(ctx context.Context, key string) (io.ReadCloser, error) {
	open := r.openerFor(key)
	policy := r.policy
	policy.OnRetry = func(attempt int, offset int64, err error) {
		r.publishRetry(key, attempt, offset, err)
	}
	var resumable *stream.Resumable
	err := r.runWithRetry(ctx, "getstream:"+key, func() error {
		var openErr error
		resumable, openErr = stream.NewResumable(ctx, policy, open)
		return openErr
	})
	if err != nil {
		return nil, err
	}
	return resumable, nil
}

func (r *RetryingStorage) openerFor(key string) stream.Opener {
	if rg, ok := r.inner.(RangeGetter); ok {
		return func(ctx context.Context, offset int64) (io.ReadCloser, error) {
			return rg.GetStreamRange(ctx, key, offset)
		}
	}
	return func(ctx context.Context, offset int64) (io.ReadCloser, error) {
		rc, err := r.inner.GetStream(ctx, key)
		if err != nil || offset == 0 {
			return rc, err
		}
		if _, skipErr := io.CopyN(io.Discard, rc, offset); skipErr != nil {
			_ = rc.Close()
			return nil, skipErr
		}
		return rc, nil
	}
}

// PutStream retries the inner upload when body is seekable; non-seekable
// bodies upload once. SDK request-phase retry handles the first attempt's
// rewind for adapters that do their own retrying — this decorator catches
// the final post-SDK transient error.
func (r *RetryingStorage) PutStream(ctx context.Context, key string, body io.Reader) error {
	seeker, seekable := body.(io.Seeker)
	if !seekable {
		return r.inner.PutStream(ctx, key, body)
	}
	return r.runWithRetry(ctx, "putstream:"+key, func() error {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return err
		}
		return r.inner.PutStream(ctx, key, body)
	})
}

// Exists reports whether key exists, retrying on transient errors.
func (r *RetryingStorage) Exists(ctx context.Context, key string) (bool, error) {
	var hit bool
	err := r.runWithRetry(ctx, "exists:"+key, func() error {
		v, e := r.inner.Exists(ctx, key)
		hit = v
		return e
	})
	return hit, err
}

// Delete removes key, retrying on transient errors.
func (r *RetryingStorage) Delete(ctx context.Context, key string) error {
	return r.runWithRetry(ctx, "delete:"+key, func() error { return r.inner.Delete(ctx, key) })
}

// DeleteBatch removes all keys in one request, retrying on transient errors.
func (r *RetryingStorage) DeleteBatch(ctx context.Context, keys []string) error {
	return r.runWithRetry(ctx, "deletebatch", func() error { return r.inner.DeleteBatch(ctx, keys) })
}

// List returns keys matching prefix, retrying on transient errors.
func (r *RetryingStorage) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	err := r.runWithRetry(ctx, "list:"+prefix, func() error {
		v, e := r.inner.List(ctx, prefix)
		keys = v
		return e
	})
	return keys, err
}

// Copy copies src to dst, retrying on transient errors.
func (r *RetryingStorage) Copy(ctx context.Context, src, dst string) error {
	return r.runWithRetry(ctx, "copy:"+src+"->"+dst, func() error { return r.inner.Copy(ctx, src, dst) })
}

// runWithRetry runs op, retrying on classified transient errors up to
// policy.MaxAttempts. Op is responsible for any per-attempt rewind. The
// label identifies the operation in retry events for observability.
func (r *RetryingStorage) runWithRetry(ctx context.Context, label string, op func() error) error {
	var lastErr error
	for attempt := range r.policy.MaxAttempts {
		if attempt > 0 {
			r.publishRetry(label, attempt, 0, lastErr)
			if err := r.policy.Sleep(ctx, backoffFor(r.policy, attempt)); err != nil {
				return err
			}
		}
		err := op()
		if err == nil {
			return nil
		}
		if !r.policy.Classify(err) {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (r *RetryingStorage) publishRetry(key string, attempt int, offset int64, cause error) {
	if r.bus == nil {
		return
	}
	r.bus.Publish(StorageRetryInfo{
		Store:       fmt.Sprint(r.inner),
		Key:         key,
		Attempt:     attempt,
		MaxAttempts: r.policy.MaxAttempts,
		Offset:      offset,
		Err:         cause,
	})
}

// backoffFor mirrors stream.backoffFor; duplicated to keep the policy contract
// internal to the retry path without exporting a helper from the stream
// package solely for call-level retry.
func backoffFor(p stream.RetryPolicy, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := p.BaseBackoff << (attempt - 1)
	if d <= 0 || d > p.MaxBackoff {
		return p.MaxBackoff
	}
	return d
}

// DefaultRetryPolicy is the production policy: 3 attempts, 250 ms → 4 s
// exponential backoff, classifier accepts the body-phase transient errors
// the SDK can't recover from. real-time backoff via time.Sleep.
func DefaultRetryPolicy() stream.RetryPolicy {
	return stream.RetryPolicy{
		MaxAttempts: 3,
		BaseBackoff: 250 * time.Millisecond,
		MaxBackoff:  4 * time.Second,
		Classify:    DefaultRetryClassify,
		Sleep:       sleepCtx,
	}
}

// DefaultRetryClassify reports whether err is the body-phase transient class
// that the SDK's request-phase retry cannot recover from. EOF without
// UnexpectedEOF is treated as legitimate end-of-stream — terminal, not
// transient.
//
// Connection-reset class (ECONNRESET / EPIPE) is retried via a narrow
// *net.OpError + syscall.Errno probe. The wider "any net.Error is transient"
// alternative was rejected because *net.DNSError is also a net.Error and a
// permanent name-resolution failure should surface immediately, not after
// MaxAttempts × MaxBackoff worth of futile retries.
func DefaultRetryClassify(err error) bool {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() >= 500 {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorFault() == smithy.FaultServer {
		return true
	}
	return false
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

var _ ports.StorageRepository = (*RetryingStorage)(nil)
