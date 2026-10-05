package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Instance struct {
	ID       string
	OwnerID  string
	Name     string
	Status   string
	Provider string
	// MachineID and VolumeID are nil until the provider has created them.
	MachineID *string
	VolumeID  *string
	CPUs      int
	MemoryMB  int
	DiskGB    int
	// Stack is the toolchain the machine was created with.
	Stack     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrInstanceNameTaken is returned when the owner already has an instance
// with the same name.
var ErrInstanceNameTaken = errors.New("instance name already in use")

const instanceColumns = `id, owner_id, name, status, provider, machine_id, volume_id, cpus, memory_mb, disk_gb, stack, created_at, updated_at`

type InstanceRepository struct {
	db *pgxpool.Pool
}

func NewInstanceRepository(db *pgxpool.Pool) *InstanceRepository {
	return &InstanceRepository{
		db: db,
	}
}

// Create inserts inst, filling in its ID, CreatedAt and UpdatedAt.
// guestTokenHash is stored alongside it but never read back into an Instance;
// use GuestTokenHash to fetch it.
func (r *InstanceRepository) Create(ctx context.Context, inst *Instance, guestTokenHash []byte) error {
	inst.ID = uuid.NewString()

	err := r.db.QueryRow(ctx,
		`INSERT INTO instances (id, owner_id, name, status, provider, machine_id, volume_id, cpus, memory_mb, disk_gb, stack, guest_token_hash)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING created_at, updated_at`,
		inst.ID, inst.OwnerID, inst.Name, inst.Status, inst.Provider, inst.MachineID, inst.VolumeID, inst.CPUs, inst.MemoryMB, inst.DiskGB, inst.Stack, guestTokenHash,
	).Scan(&inst.CreatedAt, &inst.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrInstanceNameTaken
		}
		return fmt.Errorf("create instance: %w", err)
	}

	return nil
}

// GetByID returns the instance only if it belongs to ownerID. An instance
// owned by someone else is reported as ErrNotFound, the same as a missing one,
// so callers can't probe for other users' IDs.
func (r *InstanceRepository) GetByID(ctx context.Context, id, ownerID string) (*Instance, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+instanceColumns+`
		 FROM instances
		 WHERE id = $1 AND owner_id = $2`,
		id, ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("get instance by id: %w", err)
	}

	inst, err := pgx.CollectExactlyOneRow(rows, scanInstance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get instance by id: %w", err)
	}

	return inst, nil
}

// GetForMachine returns the instance whoever owns it. It is for requests
// made by the machine itself, which has proved it is that instance; never for
// answering a user's request.
func (r *InstanceRepository) GetForMachine(ctx context.Context, id string) (*Instance, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+instanceColumns+`
		 FROM instances
		 WHERE id = $1`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("get instance: %w", err)
	}

	inst, err := pgx.CollectExactlyOneRow(rows, scanInstance)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get instance: %w", err)
	}

	return inst, nil
}

// GuestTokenHash returns the stored hash of the instance's guest token. It is
// not owner-scoped: the caller is the machine itself, not a user.
func (r *InstanceRepository) GuestTokenHash(ctx context.Context, id string) ([]byte, error) {
	var hash []byte

	err := r.db.QueryRow(ctx,
		`SELECT guest_token_hash
		 FROM instances
		 WHERE id = $1`,
		id,
	).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get guest token hash: %w", err)
	}

	return hash, nil
}

// ListByStatus returns every instance in the given status, whoever owns it.
// It is for background work, never for answering a user's request.
func (r *InstanceRepository) ListByStatus(ctx context.Context, status string) ([]*Instance, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+instanceColumns+`
		 FROM instances
		 WHERE status = $1
		 ORDER BY created_at`,
		status,
	)
	if err != nil {
		return nil, fmt.Errorf("list instances by status: %w", err)
	}

	instances, err := pgx.CollectRows(rows, scanInstance)
	if err != nil {
		return nil, fmt.Errorf("list instances by status: %w", err)
	}

	return instances, nil
}

// ListByOwner returns the owner's instances, oldest first.
func (r *InstanceRepository) ListByOwner(ctx context.Context, ownerID string) ([]*Instance, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+instanceColumns+`
		 FROM instances
		 WHERE owner_id = $1
		 ORDER BY created_at`,
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}

	instances, err := pgx.CollectRows(rows, scanInstance)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}

	return instances, nil
}

func (r *InstanceRepository) UpdateStatus(ctx context.Context, id, status string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE instances
		 SET status = $2, updated_at = NOW()
		 WHERE id = $1`,
		id, status,
	)
	if err != nil {
		return fmt.Errorf("update instance status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkProvisioned records the provider's IDs for the instance and moves it to
// status in a single statement, so a row never has a status that implies a
// machine without also having the machine's ID.
func (r *InstanceRepository) MarkProvisioned(ctx context.Context, id, machineID, volumeID, status string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE instances
		 SET machine_id = $2, volume_id = $3, status = $4, updated_at = NOW()
		 WHERE id = $1`,
		id, machineID, volumeID, status,
	)
	if err != nil {
		return fmt.Errorf("mark instance provisioned: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// TransitionStatus moves the instance to status to, but only if its current
// status is one of from. It reports whether the row was updated. The check and
// the update are one statement, so of two concurrent callers exactly one wins.
func (r *InstanceRepository) TransitionStatus(ctx context.Context, id string, from []string, to string) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE instances
		 SET status = $3, updated_at = NOW()
		 WHERE id = $1 AND status = ANY($2)`,
		id, from, to,
	)
	if err != nil {
		return false, fmt.Errorf("transition instance status: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

// SetStatusWhere moves every instance whose status is one of from to status
// to, returning how many rows changed.
func (r *InstanceRepository) SetStatusWhere(ctx context.Context, from []string, to string) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE instances
		 SET status = $2, updated_at = NOW()
		 WHERE status = ANY($1)`,
		from, to,
	)
	if err != nil {
		return 0, fmt.Errorf("set instance status: %w", err)
	}

	return tag.RowsAffected(), nil
}

func (r *InstanceRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM instances WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// scanInstance reads one row selected with instanceColumns.
func scanInstance(row pgx.CollectableRow) (*Instance, error) {
	var inst Instance
	err := row.Scan(
		&inst.ID, &inst.OwnerID, &inst.Name, &inst.Status, &inst.Provider,
		&inst.MachineID, &inst.VolumeID, &inst.CPUs, &inst.MemoryMB, &inst.DiskGB, &inst.Stack,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &inst, nil
}
