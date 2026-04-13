package internal

// Dependency anchors for delta sync v2.
// These imports prevent go mod tidy from removing pre-installed dependencies
// before implementation code is written. Remove this file after implementation.

import (
	_ "github.com/avast/retry-go/v4"
	_ "github.com/cespare/xxhash/v2"
)
