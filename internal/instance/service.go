// Package instance owns the lifecycle of user machines: it decides which
// transitions are allowed, records them in Postgres and drives the sandbox
// provider to make them real.
package instance

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/HasanMal1k/coder/internal/guestapi"
	"github.com/HasanMal1k/coder/internal/sandbox"
	"github.com/HasanMal1k/coder/internal/store"
	"github.com/HasanMal1k/coder/internal/tunnel"
)

// Statuses an instance moves through. The ones ending in -ing are
// transitional: an operation is in flight and no other may start.
const (
	StatusCreating = "creating"
	StatusStopped  = "stopped"
	StatusStarting = "starting"
	StatusRunning  = "running"
	StatusStopping = "stopping"
	StatusDeleting = "deleting"
	StatusError    = "error"
)

// Resources every instance gets for now. They become per-plan once there are
// plans.
const (
	defaultCPUs     = 2
	defaultMemoryMB = 2048
	defaultDiskGB   = 10
)

// operationTimeout bounds a single provider operation.
const operationTimeout = 2 * time.Minute

var (
	// ErrNotFound is returned when the instance does not exist or belongs to
	// another user. The two are deliberately indistinguishable.
	ErrNotFound = errors.New("instance not found")
	// ErrNameTaken is returned when the owner already has an instance with
	// that name.
	ErrNameTaken = errors.New("instance name already in use")
	// ErrInvalidName is returned when a name is not 1-40 lowercase letters,
	// digits or hyphens, starting and ending with a letter or digit.
	ErrInvalidName = errors.New("invalid instance name")
	// ErrBusy is returned when another operation on the instance is in flight.
	ErrBusy = errors.New("instance is busy")
	// ErrNotProvisioned is returned when the instance has no machine, because
	// creating it failed. Such an instance can only be deleted.
	ErrNotProvisioned = errors.New("instance was never provisioned")
	// ErrNotRunning is returned when an operation needs a running instance.
	ErrNotRunning = errors.New("instance is not running")
	// ErrGuestOffline is returned when the instance is running but its guest
	// daemon has not connected.
	ErrGuestOffline = errors.New("instance is not reachable")
	// ErrGuestUnauthorized is returned when a machine presents the wrong
	// credentials for the instance it claims to be.
	ErrGuestUnauthorized = errors.New("invalid guest credentials")
	// ErrPortClosed is returned when nothing on the machine is listening on
	// the requested port.
	ErrPortClosed = errors.New("nothing is listening on that port")
	// ErrPortUnsupported is returned when the machine was created from an
	// image whose guest daemon predates port forwarding.
	ErrPortUnsupported = errors.New("instance does not support port forwarding")
)

var validName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)

type Config struct {
	// Provider is the name stored on each row, e.g. "docker".
	Provider string
	// Image is the machine image new instances boot from.
	Image string
	// GuestConnectURL is where a machine's guest daemon dials the control
	// plane. It must be reachable from inside the machine.
	GuestConnectURL string
}

type Service struct {
	repo        *store.InstanceRepository
	provisioner sandbox.Provisioner
	hub         *tunnel.Hub
	cfg         Config
}

func NewService(repo *store.InstanceRepository, provisioner sandbox.Provisioner, hub *tunnel.Hub, cfg Config) *Service {
	return &Service{
		repo:        repo,
		provisioner: provisioner,
		hub:         hub,
		cfg:         cfg,
	}
}

// Create provisions a new machine for ownerID. The instance is left stopped.
func (s *Service) Create(ctx context.Context, ownerID, name string) (*store.Instance, error) {
	name = strings.TrimSpace(name)
	if !validName.MatchString(name) {
		return nil, ErrInvalidName
	}

	// The machine gets the token; we keep only its hash, so a database leak
	// doesn't let anyone impersonate a machine.
	guestToken := rand.Text()

	inst := &store.Instance{
		OwnerID:  ownerID,
		Name:     name,
		Status:   StatusCreating,
		Provider: s.cfg.Provider,
		CPUs:     defaultCPUs,
		MemoryMB: defaultMemoryMB,
		DiskGB:   defaultDiskGB,
	}
	if err := s.repo.Create(ctx, inst, hashGuestToken(guestToken)); err != nil {
		if errors.Is(err, store.ErrInstanceNameTaken) {
			return nil, ErrNameTaken
		}
		return nil, err
	}

	ctx, cancel := operationContext(ctx)
	defer cancel()

	handle, err := s.provisioner.Create(ctx, sandbox.Spec{
		InstanceID: inst.ID,
		OwnerID:    inst.OwnerID,
		// Instance names are valid hostnames by construction (see validName).
		Hostname: inst.Name,
		Image:    s.cfg.Image,
		Resources: sandbox.Resources{
			CPUs:     inst.CPUs,
			MemoryMB: inst.MemoryMB,
			DiskGB:   inst.DiskGB,
		},
		Env: map[string]string{
			guestapi.EnvInstanceID: inst.ID,
			guestapi.EnvGuestToken: guestToken,
			guestapi.EnvConnectURL: s.cfg.GuestConnectURL,
		},
	})
	if err != nil {
		return nil, s.fail(ctx, inst.ID, fmt.Errorf("provision instance: %w", err))
	}

	if err := s.repo.MarkProvisioned(ctx, inst.ID, handle.MachineID, handle.VolumeID, StatusStopped); err != nil {
		// The machine exists but we couldn't record it. Remove it rather
		// than leave something running that nothing points to.
		err = errors.Join(err, s.provisioner.Delete(ctx, handle))
		return nil, s.fail(ctx, inst.ID, err)
	}

	return s.Get(ctx, ownerID, inst.ID)
}

func (s *Service) Get(ctx context.Context, ownerID, id string) (*store.Instance, error) {
	// Anything that isn't a UUID can't be an instance; Postgres would reject
	// it with an error rather than simply finding nothing.
	if uuid.Validate(id) != nil {
		return nil, ErrNotFound
	}

	inst, err := s.repo.GetByID(ctx, id, ownerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return inst, nil
}

func (s *Service) List(ctx context.Context, ownerID string) ([]*store.Instance, error) {
	return s.repo.ListByOwner(ctx, ownerID)
}

// Start boots the instance's machine. Starting a running instance is a no-op.
func (s *Service) Start(ctx context.Context, ownerID, id string) (*store.Instance, error) {
	inst, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}
	if inst.Status == StatusRunning {
		return inst, nil
	}
	if inst.MachineID == nil {
		return nil, notProvisionedOrBusy(inst)
	}

	if err := s.begin(ctx, id, []string{StatusStopped, StatusError}, StatusStarting); err != nil {
		return nil, err
	}

	ctx, cancel := operationContext(ctx)
	defer cancel()

	if err := s.provisioner.Start(ctx, handleOf(inst)); err != nil {
		return nil, s.fail(ctx, id, fmt.Errorf("start instance: %w", err))
	}
	if err := s.repo.UpdateStatus(ctx, id, StatusRunning); err != nil {
		return nil, err
	}

	return s.Get(ctx, ownerID, id)
}

// Stop halts the instance's machine, keeping its volume. Stopping a stopped
// instance is a no-op.
func (s *Service) Stop(ctx context.Context, ownerID, id string) (*store.Instance, error) {
	inst, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}
	if inst.Status == StatusStopped {
		return inst, nil
	}
	if inst.MachineID == nil {
		return nil, notProvisionedOrBusy(inst)
	}

	if err := s.begin(ctx, id, []string{StatusRunning, StatusError}, StatusStopping); err != nil {
		return nil, err
	}

	ctx, cancel := operationContext(ctx)
	defer cancel()

	if err := s.provisioner.Stop(ctx, handleOf(inst)); err != nil {
		return nil, s.fail(ctx, id, fmt.Errorf("stop instance: %w", err))
	}
	if err := s.repo.UpdateStatus(ctx, id, StatusStopped); err != nil {
		return nil, err
	}

	return s.Get(ctx, ownerID, id)
}

// Delete removes the instance, its machine and its volume permanently.
func (s *Service) Delete(ctx context.Context, ownerID, id string) error {
	inst, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return err
	}

	if err := s.begin(ctx, id, []string{StatusStopped, StatusRunning, StatusError}, StatusDeleting); err != nil {
		return err
	}

	ctx, cancel := operationContext(ctx)
	defer cancel()

	if err := s.provisioner.Delete(ctx, handleOf(inst)); err != nil {
		return s.fail(ctx, id, fmt.Errorf("delete instance: %w", err))
	}

	return s.repo.Delete(ctx, id)
}

// OpenTerminal starts a shell on the owner's running instance. If the machine
// was only just started, it waits a short while for its guest to connect.
func (s *Service) OpenTerminal(ctx context.Context, ownerID, id string, size tunnel.Size) (*tunnel.Terminal, error) {
	inst, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}
	if inst.Status != StatusRunning {
		return nil, ErrNotRunning
	}

	term, err := s.hub.OpenTerminal(ctx, inst.ID, size)
	if err != nil {
		if errors.Is(err, tunnel.ErrGuestOffline) {
			return nil, ErrGuestOffline
		}
		return nil, err
	}

	return term, nil
}

// DialPort connects to a port on instance id's loopback interface. It does no
// ownership check: the caller must already have established that whoever is
// asking may use this instance.
func (s *Service) DialPort(ctx context.Context, id string, port int) (net.Conn, error) {
	conn, err := s.hub.DialPort(ctx, id, port)
	if err != nil {
		switch {
		case errors.Is(err, tunnel.ErrGuestOffline):
			return nil, ErrGuestOffline
		case errors.Is(err, tunnel.ErrPortClosed):
			return nil, ErrPortClosed
		case errors.Is(err, tunnel.ErrUnsupported):
			return nil, ErrPortUnsupported
		}
		return nil, err
	}

	return conn, nil
}

// AuthenticateGuest checks that token is the guest token issued to instance
// id. A wrong token and an unknown instance are indistinguishable.
func (s *Service) AuthenticateGuest(ctx context.Context, id, token string) error {
	if uuid.Validate(id) != nil || token == "" {
		return ErrGuestUnauthorized
	}

	want, err := s.repo.GuestTokenHash(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrGuestUnauthorized
		}
		return err
	}

	// Constant-time, so response timing reveals nothing about the hash.
	if subtle.ConstantTimeCompare(hashGuestToken(token), want) != 1 {
		return ErrGuestUnauthorized
	}

	return nil
}

// ServeGuest runs the tunnel to instance id's guest over conn, blocking until
// it disconnects. The guest must already have passed AuthenticateGuest.
func (s *Service) ServeGuest(ctx context.Context, id string, conn net.Conn) error {
	// Register under the same canonical form of the ID that Postgres returns,
	// which is what OpenTerminal looks guests up by.
	parsed, err := uuid.Parse(id)
	if err != nil {
		conn.Close()
		return ErrGuestUnauthorized
	}

	return s.hub.Serve(ctx, parsed.String(), conn)
}

// RecoverInterrupted marks instances left in a transitional status as failed.
// Call it once at startup, before serving requests: operations only run inside
// this process, so anything still transitional was cut off by a crash or
// restart. Users can then retry or delete those instances.
func (s *Service) RecoverInterrupted(ctx context.Context) (int64, error) {
	return s.repo.SetStatusWhere(ctx,
		[]string{StatusCreating, StatusStarting, StatusStopping, StatusDeleting},
		StatusError,
	)
}

// begin claims the instance for an operation by moving it from one of the
// from statuses to a transitional one. Only one caller can win the claim, so
// concurrent requests for the same instance can't run two operations at once.
func (s *Service) begin(ctx context.Context, id string, from []string, to string) error {
	ok, err := s.repo.TransitionStatus(ctx, id, from, to)
	if err != nil {
		return err
	}
	if !ok {
		return ErrBusy
	}

	return nil
}

// fail marks the instance as errored and returns cause, joined with the
// bookkeeping error if marking it failed too.
func (s *Service) fail(ctx context.Context, id string, cause error) error {
	if err := s.repo.UpdateStatus(ctx, id, StatusError); err != nil {
		return errors.Join(cause, fmt.Errorf("mark instance errored: %w", err))
	}

	return cause
}

// operationContext returns a context for a provider operation that outlives
// the request that triggered it. Once an operation has begun it must run to
// completion even if the client disconnects, otherwise the instance is left
// stuck in a transitional status.
func operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), operationTimeout)
}

func hashGuestToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func handleOf(inst *store.Instance) sandbox.Handle {
	var h sandbox.Handle
	if inst.MachineID != nil {
		h.MachineID = *inst.MachineID
	}
	if inst.VolumeID != nil {
		h.VolumeID = *inst.VolumeID
	}

	return h
}

// notProvisionedOrBusy explains why an instance without a machine can't be
// started or stopped: either it is still being created, or creation failed.
func notProvisionedOrBusy(inst *store.Instance) error {
	if inst.Status == StatusError {
		return ErrNotProvisioned
	}

	return ErrBusy
}
