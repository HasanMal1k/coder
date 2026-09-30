// Package sandbox abstracts how a user's environment is actually run. The
// instance service talks only to Provisioner, so the Docker implementation can
// later be swapped for Firecracker microVMs without touching callers.
package sandbox

import (
	"context"
	"errors"
	"io"
)

var (
	// ErrNotFound is returned when no sandbox exists for the given ID, e.g. the
	// container was removed out from under us.
	ErrNotFound = errors.New("sandbox not found")
	// ErrNotRunning is returned by Attach when the sandbox is not running.
	ErrNotRunning = errors.New("sandbox not running")
)

// State is the runtime state of a sandbox as reported by the provisioner. It
// is deliberately coarse: finer lifecycle states (e.g. "starting") belong to
// the instance domain, not here.
type State string

const (
	StateCreated State = "created" // exists but has never been started
	StateRunning State = "running"
	StateStopped State = "stopped" // was started, is not running now
)

// Resources are the limits applied to a sandbox.
type Resources struct {
	// CPUMillis is CPU in thousandths of a core: 1000 = 1 vCPU.
	CPUMillis int64
	// MemoryBytes is a hard memory limit.
	MemoryBytes int64
}

// Spec describes a sandbox to create.
type Spec struct {
	// InstanceID is our ID for the instance. Implementations use it to name
	// and label what they create, so resources can be traced back to it.
	InstanceID string
	OwnerID    string
	Image      string
	Resources  Resources
}

// AttachOptions configure a terminal session inside a running sandbox.
type AttachOptions struct {
	// Cmd is the process to run, e.g. []string{"/bin/bash", "-l"}.
	Cmd  []string
	Rows uint16
	Cols uint16
}

// Session is an interactive terminal inside a sandbox. Writes go to the
// process's stdin; reads return its combined stdout/stderr (it runs under a
// TTY). Read returns io.EOF once the process exits. Close ends the session
// but leaves the sandbox running.
type Session interface {
	io.ReadWriteCloser
	Resize(ctx context.Context, rows, cols uint16) error
}

// Provisioner creates and controls sandboxes. IDs are opaque to callers; they
// are whatever the implementation returned from Create.
//
// Start, Stop and Delete are idempotent: starting a running sandbox, stopping
// a stopped one or deleting a missing one returns nil, so callers can safely
// retry after partial failures.
type Provisioner interface {
	// Create provisions a sandbox with persistent storage but does not start it.
	Create(ctx context.Context, spec Spec) (id string, err error)
	Start(ctx context.Context, id string) error
	// Stop halts the sandbox. Its storage persists across Stop/Start.
	Stop(ctx context.Context, id string) error
	// Delete removes the sandbox and its storage permanently.
	Delete(ctx context.Context, id string) error
	State(ctx context.Context, id string) (State, error)
	// Attach starts a new terminal session in a running sandbox.
	Attach(ctx context.Context, id string, opts AttachOptions) (Session, error)
}
