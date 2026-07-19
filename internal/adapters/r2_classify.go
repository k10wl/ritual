package adapters

import (
	"errors"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// isNotFound reports whether err describes a missing S3 object (used by
// HeadObject-based Exists). R2 surfaces 404s as APIError "NotFound" or
// "NoSuchKey" depending on operation.
func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "NotFound" || code == "NoSuchKey" {
			return true
		}
	}
	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == 404 {
		return true
	}
	return false
}
