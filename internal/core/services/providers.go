package services

// Provider interfaces consumed by the pre-flight checks. Implementations
// live in the adapters package; tests use small fakes.

// SystemInfoProvider abstracts host RAM probing for testability.
type SystemInfoProvider interface {
	GetFreeRAMMB() (int, error)
}

// DiskInfoProvider abstracts per-volume free-space probing for testability.
type DiskInfoProvider interface {
	GetFreeDiskMB(path string) (int, error)
}

// JavaVersionProvider abstracts Java major-version detection for testability.
type JavaVersionProvider interface {
	GetJavaVersion() (int, error)
}
