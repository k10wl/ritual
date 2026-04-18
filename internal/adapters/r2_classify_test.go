package adapters

import (
	"errors"
	"net"
	"net/http"
	"ritual/internal/adapters/retry"
	"testing"
	"time"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type fakeNetErr struct{ timeout bool }

func (f fakeNetErr) Error() string   { return "fake net err" }
func (f fakeNetErr) Timeout() bool   { return f.timeout }
func (f fakeNetErr) Temporary() bool { return false }

func TestR2Retryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"fatal marker", retry.Fatal(errors.New("bug")), false},
		{"plain error (unknown)", errors.New("mystery"), true},

		// Permanent SDK codes
		{"NoSuchKey", &smithy.GenericAPIError{Code: "NoSuchKey"}, false},
		{"NoSuchBucket", &smithy.GenericAPIError{Code: "NoSuchBucket"}, false},
		{"AccessDenied", &smithy.GenericAPIError{Code: "AccessDenied"}, false},
		{"InvalidAccessKeyId", &smithy.GenericAPIError{Code: "InvalidAccessKeyId"}, false},
		{"SignatureDoesNotMatch", &smithy.GenericAPIError{Code: "SignatureDoesNotMatch"}, false},

		// Retryable SDK codes
		{"SlowDown", &smithy.GenericAPIError{Code: "SlowDown"}, true},
		{"RequestTimeout", &smithy.GenericAPIError{Code: "RequestTimeout"}, true},
		{"InternalError", &smithy.GenericAPIError{Code: "InternalError"}, true},
		{"ServiceUnavailable", &smithy.GenericAPIError{Code: "ServiceUnavailable"}, true},

		// HTTP status classification
		{"HTTP 500", httpRespErr(500), true},
		{"HTTP 502", httpRespErr(502), true},
		{"HTTP 503", httpRespErr(503), true},
		{"HTTP 408", httpRespErr(408), true},
		{"HTTP 429", httpRespErr(429), true},
		{"HTTP 400", httpRespErr(400), false},
		{"HTTP 401", httpRespErr(401), false},
		{"HTTP 404", httpRespErr(404), false},

		// Net errors
		{"net.Error timeout", fakeNetErr{timeout: true}, true},
		{"net.Error non-timeout", fakeNetErr{}, true},
		{"net.OpError", &net.OpError{Op: "dial", Err: errors.New("conn refused")}, true},

		// Wrapped cases (ensure errors.As works)
		{"wrapped permanent", wrap(&smithy.GenericAPIError{Code: "AccessDenied"}), false},
		{"wrapped retryable", wrap(&smithy.GenericAPIError{Code: "SlowDown"}), true},
		{"wrapped fatal", wrap(retry.Fatal(errors.New("bug"))), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := r2Retryable(tc.err); got != tc.want {
				t.Errorf("r2Retryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func httpRespErr(status int) error {
	return &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{
			Response: &http.Response{StatusCode: status},
		},
		Err: errors.New("http response error"),
	}
}

func wrap(err error) error {
	return &wrappedErr{err: err}
}

type wrappedErr struct{ err error }

func (w *wrappedErr) Error() string { return "wrapped: " + w.err.Error() }
func (w *wrappedErr) Unwrap() error { return w.err }

// Sanity: fakeNetErr must satisfy net.Error.
var _ net.Error = fakeNetErr{}

// Sanity: durations used nowhere in classifier, but ensure stdlib is imported cleanly.
var _ = time.Second
