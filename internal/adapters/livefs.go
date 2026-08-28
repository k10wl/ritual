package adapters

import (
	"io/fs"
	"os"
)

// liveDirFS is an fs.FS whose Open re-resolves pathFn() on every call
// instead of baking a directory string in at construction — needed because
// FullScanner is built once at boot and held forever by applier/committer
// (design-log/055 Q4: a relocate must not leave them silently reading/
// writing the old work root).
type liveDirFS struct {
	pathFn func() string
}

// LiveDirFS returns an fs.FS backed by whatever pathFn() returns at the
// moment of each call, not at construction time.
func LiveDirFS(pathFn func() string) fs.FS {
	return liveDirFS{pathFn: pathFn}
}

func (l liveDirFS) Open(name string) (fs.File, error) {
	return os.DirFS(l.pathFn()).Open(name)
}

var _ fs.FS = liveDirFS{}
