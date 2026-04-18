package adapters

import (
	"errors"
	"net"
	"ritual/internal/adapters/retry"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// r2Retryable decides whether an error returned by S3Client (R2) is worth retrying.
// Called from rg.RetryIf inside each R2Repository method.
//
// Order of precedence:
//  1. nil / retry.Fatal → never retry.
//  2. Known permanent SDK error codes (NoSuchKey, AccessDenied, …) → no retry.
//  3. HTTP 5xx / 408 / 429 → retry.
//  4. HTTP 4xx (excluding 408, 429) → no retry.
//  5. net.Error (timeouts, conn reset) → retry.
//  6. Unknown → retry (optimistic; flip if noisy in prod).
func r2Retryable(err error) bool {
	if err == nil {
		return false
	}
	if retry.IsFatal(err) {
		return false
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NoSuchBucket", "AccessDenied",
			"InvalidAccessKeyId", "SignatureDoesNotMatch",
			"InvalidBucketName", "InvalidRequest",
			"MethodNotAllowed", "NotImplemented":
			return false
		case "SlowDown", "RequestTimeout", "RequestTimeTooSkewed",
			"InternalError", "ServiceUnavailable":
			return true
		}
	}

	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) {
		status := respErr.HTTPStatusCode()
		if status >= 500 || status == 408 || status == 429 {
			return true
		}
		if status >= 400 {
			return false
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	return true
}
