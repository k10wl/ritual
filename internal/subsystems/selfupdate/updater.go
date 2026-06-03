package selfupdate

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/minio/selfupdate"

	"ritual/internal/core/ports"
)

// Updater is the pure port impl of ports.UpdaterService (design-log/037). It
// owns the version decision (listing-derived latest) and the relaunch; the
// byte-level atomic replace + checksum + rollback is delegated to
// minio/selfupdate. No event bus here — observability is the observed.Updater
// decorator's job, mirroring observed.Locker/Retention.
type Updater struct {
	remote   ports.StorageRepository // the observed + counter-wrapped remoteStorage
	current  string                  // config.AppVersion — the running binary's semver
	prefix   string                  // "bin/<goos>-<goarch>/" — this client's platform
	relaunch func() error            // exec(os.Executable()) + wailsApp.Quit(); may be nil in tests

	// applyFn performs the byte replace. Defaults to selfupdate.Apply;
	// injectable so unit tests exercise download + checksum wiring without
	// replacing the test binary.
	applyFn func(update io.Reader, opts selfupdate.Options) error
}

// New builds an Updater for the running platform. relaunch is invoked after a
// successful Apply; pass nil to skip relaunch (tests).
func New(remote ports.StorageRepository, current, goos, goarch string, relaunch func() error) *Updater {
	return &Updater{
		remote:   remote,
		current:  current,
		prefix:   PrefixFor(goos, goarch),
		relaunch: relaunch,
		applyFn:  selfupdate.Apply,
	}
}

var _ ports.UpdaterService = (*Updater)(nil)

// Check lists this platform's artifacts and reports the latest plus whether
// the running binary is older than it. An empty/missing prefix yields
// (Update{}, false, nil) — nothing to update to.
func (u *Updater) Check(ctx context.Context) (ports.Update, bool, error) {
	keys, err := u.remote.List(ctx, u.prefix)
	if err != nil {
		return ports.Update{}, false, fmt.Errorf("selfupdate: list %s: %w", u.prefix, err)
	}
	up := latest(u.prefix, keys)
	if up.Version == "" {
		return ports.Update{}, false, nil
	}
	return up, IsVersionOlder(u.current, up.Version), nil
}

// Apply streams the artifact, hands it to minio/selfupdate with the expected
// sha256 (the key's leaf) for verification + atomic rename + rollback, then
// relaunches. On success the process is replaced/quit, so Apply does not
// return. A checksum mismatch or download error leaves the running binary
// intact (minio rolled back) and surfaces as an error.
func (u *Updater) Apply(ctx context.Context, up ports.Update) error {
	sum, err := hex.DecodeString(up.SHA256)
	if err != nil {
		return fmt.Errorf("selfupdate: bad checksum %q: %w", up.SHA256, err)
	}
	body, err := u.remote.GetStream(ctx, up.Key)
	if err != nil {
		return fmt.Errorf("selfupdate: download %s: %w", up.Key, err)
	}
	defer func() { _ = body.Close() }()

	if err := u.applyFn(body, selfupdate.Options{Checksum: sum}); err != nil {
		return fmt.Errorf("selfupdate: apply %s: %w", up.Version, err)
	}
	if u.relaunch == nil {
		return nil
	}
	return u.relaunch()
}
