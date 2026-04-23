package refs

import "errors"

// ErrBlobDownload classifies a failure while streaming the source body
// into the destination. The destination is scrubbed before this surfaces
// so the next Pull starts from a clean Exists == false state.
var ErrBlobDownload = errors.New("refs: blob download failed")

// ErrBlobCleanup classifies a failed Delete during recovery. The
// destination may still hold stale or partial bytes; rerunning Pull
// alone may not resolve it. Surface for operator attention — never
// suppress.
var ErrBlobCleanup = errors.New("refs: blob cleanup failed")
