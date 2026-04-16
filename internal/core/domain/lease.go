package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Duration is a time.Duration that marshals to/from Go duration strings
// ("1m", "10m", "1h30m") in JSON. Zero value marshals as empty string and
// omits when paired with omitempty.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// LeaseSettings configure the remote-lock heartbeat lease. Values are
// read once at Acquiring from the remote manifest and remain constant for
// the run's lock tenure. See docs/state-machine.md.
type LeaseSettings struct {
	HeartbeatInterval Duration `json:"heartbeat_interval,omitempty"`
	TTL               Duration `json:"ttl,omitempty"`
}

var (
	ErrLeaseIntervalTooSmall = errors.New("heartbeat_interval must be >= 30s")
	ErrLeaseTTLTooSmall      = errors.New("ttl must be >= 3 * heartbeat_interval")
	ErrLeaseTTLTooLarge      = errors.New("ttl must be <= 24h")
)

// Validate enforces sane bounds. Zero values are treated as unset and
// fail — callers should apply defaults first via ApplyDefaults.
func (l LeaseSettings) Validate() error {
	hb := time.Duration(l.HeartbeatInterval)
	ttl := time.Duration(l.TTL)
	if hb < 30*time.Second {
		return ErrLeaseIntervalTooSmall
	}
	if ttl < 3*hb {
		return ErrLeaseTTLTooSmall
	}
	if ttl > 24*time.Hour {
		return ErrLeaseTTLTooLarge
	}
	return nil
}
