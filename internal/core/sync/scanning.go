package sync

import (
	"context"
	"errors"
	"fmt"
	"ritual/internal/core/domain"
	"ritual/internal/core/machine"
	"ritual/internal/core/ports"
	"time"
)

// Scanning is the entry strategy. Emits SyncStartedInfo, builds SrcMap from
// the scanner, loads DstMap from the destination manifest reader, and
// routes to Planning.
type Scanning struct {
	scanner ports.DirectoryScanner
	onOK    machine.Strategy[RunState]
	onFail  machine.Strategy[RunState]
}

// NewScanning constructs a Scanning strategy. scanner produces the source
// file map. onOK runs after Planning; onFail handles errors.
func NewScanning(scanner ports.DirectoryScanner, onOK, onFail machine.Strategy[RunState]) *Scanning {
	return &Scanning{scanner: scanner, onOK: onOK, onFail: onFail}
}

// Run executes the scan + manifest load.
func (s *Scanning) Run(ctx context.Context, rs *RunState) (machine.Strategy[RunState], error) {
	rs.Started = time.Now()
	rs.Publish(SyncStartedInfo{syncBase: rs.envelope()})

	if rs.DstManifestRead == nil {
		return s.fail(rs, errors.New("dst manifest reader not wired"))
	}

	if s.scanner == nil {
		// No scanner — treat src as empty (e.g. download direction's "src=remote
		// manifest" path uses DstManifestRead from inverted wiring). For
		// symmetry, scan emits empty SrcMap.
		rs.SrcMap = map[string]domain.FileEntry{}
	}
	if s.scanner != nil {
		m, err := s.scanner.Scan(ctx)
		if err != nil {
			return s.fail(rs, fmt.Errorf("scan src: %w", err))
		}
		rs.SrcMap = m
	}

	dstMap, err := rs.DstManifestRead()
	if err != nil {
		return s.fail(rs, fmt.Errorf("read dst manifest: %w", err))
	}
	if dstMap == nil {
		dstMap = map[string]domain.FileEntry{}
	}
	rs.DstMap = dstMap

	return s.onOK, nil
}

func (s *Scanning) fail(rs *RunState, err error) (machine.Strategy[RunState], error) {
	rs.Err = err
	rs.FailedPhase = PhaseScan
	return s.onFail, nil
}
