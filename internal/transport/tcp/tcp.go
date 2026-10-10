// Package tcp implements the baseline TCP transport.
package tcp

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Transport is a minimal standard-library TCP dial/listen implementation.
type Transport struct{ DialTimeout time.Duration }

// Name returns the stable transport identifier.
func (Transport) Name() string { return "tcp" }

// Dial opens a TCP connection while respecting context cancellation.
func (t Transport) Dial(ctx context.Context, address string) (net.Conn, error) {
	timeout := t.DialTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	d := net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s: %w", address, err)
	}
	return conn, nil
}

// Listen starts a TCP listener on the given address.
func (Transport) Listen(ctx context.Context, address string) (net.Listener, error) {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("tcp listen %s: %w", address, err)
	}
	return ln, nil
}
