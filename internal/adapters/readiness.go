package adapters

import (
	"context"
	"net"
	"ritual/internal/core/ports"
	"time"
)

// TCPReadinessCheck dials a TCP address repeatedly until it accepts a connection.
type TCPReadinessCheck struct {
	address     string
	bus         ports.EventBus
	dialTimeout time.Duration
	interval    time.Duration
}

var _ ports.ReadinessCheck = (*TCPReadinessCheck)(nil)

// NewTCPReadinessCheck builds a TCPReadinessCheck with default 1s dial timeout and interval.
func NewTCPReadinessCheck(address string, bus ports.EventBus) *TCPReadinessCheck {
	return &TCPReadinessCheck{
		address:     address,
		bus:         bus,
		dialTimeout: time.Second,
		interval:    time.Second,
	}
}

// SetDialTimeout bounds a single dial attempt. Default 1s.
func (r *TCPReadinessCheck) SetDialTimeout(d time.Duration) { r.dialTimeout = d }

// SetInterval is the sleep between failed dial attempts. Default 1s.
func (r *TCPReadinessCheck) SetInterval(d time.Duration) { r.interval = d }

// Wait blocks until a TCP dial succeeds or ctx is cancelled.
func (r *TCPReadinessCheck) Wait(ctx context.Context) error {
	var attempt uint
	for {
		attempt++
		conn, err := net.DialTimeout("tcp", r.address, r.dialTimeout)
		if err == nil {
			_ = conn.Close()
			r.publish(ReadinessDialInfo{Address: r.address, Attempt: attempt})
			return nil
		}
		r.publish(ReadinessDialInfo{Address: r.address, Attempt: attempt, Err: err})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.interval):
		}
	}
}

func (r *TCPReadinessCheck) publish(evt ports.Event) {
	if r.bus == nil {
		return
	}
	r.bus.Publish(evt)
}
