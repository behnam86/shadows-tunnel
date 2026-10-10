// Package engine defines transport-independent tunnel lifecycle contracts.
package engine

import (
	"context"
	"errors"
	"net"
)

// TunnelID uniquely identifies a tunnel within an installation.
type TunnelID string

// TunnelSpec describes a tunnel without prescribing its transport implementation.
type TunnelSpec struct {
	ID       TunnelID
	Mode     string
	Protocol string
	Listen   string
	Target   string
}

// TunnelStatus reports the lifecycle state of a tunnel.
type TunnelStatus struct {
	ID    TunnelID `json:"id"`
	State string   `json:"state"`
	Error string   `json:"error,omitempty"`
}

// Listener is the minimal accept/close contract required by the engine.
type Listener interface {
	Accept() (net.Conn, error)
	Close() error
	Addr() net.Addr
}

// TunnelEngine describes lifecycle operations implemented by tunnel engines.
type TunnelEngine interface {
	Create(context.Context, TunnelSpec) (TunnelID, error)
	Start(context.Context, TunnelID) error
	Stop(context.Context, TunnelID) error
	Restart(context.Context, TunnelID) error
	Status(context.Context, TunnelID) (TunnelStatus, error)
	Delete(context.Context, TunnelID) error
}

// ErrNotImplemented marks lifecycle operations not provided by a specific engine yet.
var ErrNotImplemented = errors.New("operation not implemented by this engine")
