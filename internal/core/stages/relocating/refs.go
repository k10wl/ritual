package relocating

import (
	"os"
	"ritual/internal/adapters"
	"ritual/internal/core/ports"
	"sync/atomic"
)

// WorkRootRefs bundles the three swap points a relocate touches: the raw
// *os.Root used by localStatsFn and for building the next generation of
// FSRepositories, and the two content-storage facades everything else
// (puller, applier, committer, pusher, dirtyProber, versionLister,
// localCollector/Deleter, locker) was built on top of, once, at boot
// (design-log/055 Q4). Exported so cmd/gui.buildRuntime and
// internal/gui/control.ControlService can both hold/pass it — no *pipeline
// stage* package needs to know relocating exists, but the composition root
// and the control layer necessarily do, to construct the bundle and to
// drive a relocate.
type WorkRootRefs struct {
	Root    *atomic.Pointer[os.Root]
	Local   *adapters.SwappableStorage
	Workdir *adapters.SwappableStorage
}

type workRootSnapshot struct {
	root    *os.Root
	local   ports.StorageRepository
	workdir ports.StorageRepository
}

func (r WorkRootRefs) snapshot() workRootSnapshot {
	return workRootSnapshot{root: r.Root.Load(), local: r.Local.Current(), workdir: r.Workdir.Current()}
}

// store swaps all three refs together — the Consistency moment (Crash
// safety §, design-log/055): any read through them at any instant sees a
// self-consistent set, never a torn pre/post mix.
func (r WorkRootRefs) store(root *os.Root, local, workdir ports.StorageRepository) {
	r.Root.Store(root)
	r.Local.Store(local)
	r.Workdir.Store(workdir)
}
