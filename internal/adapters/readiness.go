package adapters

import (
	"context"
	"net"
	"time"

	"ritual/internal/core/ports"
)

type TCPReadinessCheck struct {
	address     string
	dialTimeout time.Duration
	interval    time.Duration
}

var _ ports.ReadinessCheck = (*TCPReadinessCheck)(nil)

func NewTCPReadinessCheck(address string) *TCPReadinessCheck {
	return &TCPReadinessCheck{
		address:     address,
		dialTimeout: time.Second,
		interval:    time.Second,
	}
}

func (r *TCPReadinessCheck) Wait(ctx context.Context) error {
	for {
		conn, err := net.DialTimeout("tcp", r.address, r.dialTimeout)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.interval):
		}
	}
}
