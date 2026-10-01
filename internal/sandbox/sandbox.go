// Package sandbox abstracts where a user's machine runs. It covers lifecycle
// only: creating, starting, stopping and deleting machines. Everything that
// happens inside a machine (terminals, files) goes through the guest daemon,
// not through this package, so providers stay small and swappable.
package sandbox

import (
	"context"
	"errors"
)

// ErrNotFound is returned when the provider has no machine for the given
// handle, e.g. it was removed out from under us.
var ErrNotFound = errors.New("sandbox not found")

// State is the runtime state of a machine as reported by the provider. It is
// deliberately coarse: finer lifecycle states (e.g. "starting") belong to the
// instance domain, not here.
type State string

const (
	StateCreated State = "created" // exists but has never been started
	StateRunning State = "running"
	StateStopped State = "stopped" // was started, is not running now
)

// Resources are the limits applied to a machine.
type Resources struct {
	CPUs     int
	MemoryMB int
	// DiskGB is the size of the persistent volume. Providers that cannot
	// enforce a size (local Docker volumes) ignore it.
	DiskGB int
}

// Spec describes a machine to create.
type Spec struct {
	// InstanceID is our ID for the instance. Providers use it to name and
	// label what they create, so resources can be traced back to it.
	InstanceID string
	OwnerID    string
	Image      string
	Resources  Resources
	Env        map[string]string
}

// Handle holds the provider's own IDs for a machine and its persistent
// volume. Callers store it and pass it back; they never interpret it.
type Handle struct {
	MachineID string
	VolumeID  string
}

// Provisioner creates and controls machines.
//
// Start, Stop and Delete are idempotent: starting a running machine, stopping
// a stopped one or deleting a missing one returns nil, so callers can safely
// retry after partial failures.
type Provisioner interface {
	// Create provisions a machine and its persistent volume but does not
	// start it.
	Create(ctx context.Context, spec Spec) (Handle, error)
	Start(ctx context.Context, h Handle) error
	// Stop halts the machine. Its volume persists across Stop/Start.
	Stop(ctx context.Context, h Handle) error
	// Delete removes the machine and its volume permanently.
	Delete(ctx context.Context, h Handle) error
	State(ctx context.Context, h Handle) (State, error)
}
