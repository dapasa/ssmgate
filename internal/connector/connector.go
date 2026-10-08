// Package connector defines the boundary between the CLI commands and the
// backend that actually reaches the infrastructure. Two implementations
// exist: awsx (real AWS SSM) and demo (offline simulation), so every command
// can run end-to-end without an AWS account.
package connector

import (
	"context"
	"sync"

	"github.com/dapasa/ssmgate/internal/config"
)

// Connector opens shells and database tunnels for an environment.
type Connector interface {
	// Shell opens an interactive session on a host and blocks until it ends.
	Shell(ctx context.Context, env *config.Environment, host string) error
	// OpenTunnel opens a port-forward to env.DB on localhost:env.DB.LocalPort.
	// The caller owns the returned tunnel and must Close it.
	OpenTunnel(ctx context.Context, env *config.Environment) (*Tunnel, error)
}

// Tunnel is a live local port-forward plus what a client needs to log in.
type Tunnel struct {
	LocalPort int
	DBUser    string
	// Password is the IAM auth token, or empty when auth is "password".
	Password string
	// CAPath is the root certificate for verify-* SSL modes (may be empty).
	CAPath string

	done      chan struct{}
	closeFn   func()
	closeOnce sync.Once
	doneOnce  sync.Once
}

// NewTunnel builds a Tunnel. closeFn stops the underlying forwarder; it must
// be safe to call once. Call MarkDone when the forwarder exits by itself.
func NewTunnel(closeFn func()) *Tunnel {
	return &Tunnel{done: make(chan struct{}), closeFn: closeFn}
}

// Done is closed when the forwarder stops (closed or died on its own).
func (t *Tunnel) Done() <-chan struct{} { return t.done }

// MarkDone signals that the forwarder has stopped. Safe to call repeatedly.
func (t *Tunnel) MarkDone() {
	t.doneOnce.Do(func() { close(t.done) })
}

// Close stops the forwarder (once) and waits for Done.
func (t *Tunnel) Close() {
	t.closeOnce.Do(func() {
		if t.closeFn != nil {
			t.closeFn()
		}
		t.MarkDone()
	})
}
