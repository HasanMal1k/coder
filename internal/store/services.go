package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MachineService is a program the platform keeps running on a machine.
type MachineService struct {
	InstanceID string
	Name       string
	Command    string
	// Directory is the folder the command is run in.
	Directory string
	// Port is the port it listens on; zero if it listens on none.
	Port int
	// Revision goes up every time the service is registered, changed or
	// not. It is how a machine knows to start the program afresh, and how
	// the platform knows which start it is looking at. Set by Save.
	Revision int64
}

type ServiceRepository struct {
	db *pgxpool.Pool
}

func NewServiceRepository(db *pgxpool.Pool) *ServiceRepository {
	return &ServiceRepository{
		db: db,
	}
}

// Save registers a service on a machine, replacing one of the same name.
func (r *ServiceRepository) Save(ctx context.Context, svc *MachineService) error {
	var port *int
	if svc.Port != 0 {
		port = &svc.Port
	}

	// clock_timestamp, not NOW: two registrations in one transaction, or
	// in quick succession, must still differ.
	var updated time.Time
	err := r.db.QueryRow(ctx,
		`INSERT INTO services (instance_id, name, command, directory, port, updated_at)
		 VALUES ($1, $2, $3, $4, $5, clock_timestamp())
		 ON CONFLICT (instance_id, name) DO UPDATE
		 SET command = EXCLUDED.command, directory = EXCLUDED.directory, port = EXCLUDED.port, updated_at = clock_timestamp()
		 RETURNING updated_at`,
		svc.InstanceID, svc.Name, svc.Command, svc.Directory, port,
	).Scan(&updated)
	if err != nil {
		return fmt.Errorf("save service: %w", err)
	}
	svc.Revision = updated.UnixMicro()

	return nil
}

// Delete removes a service, and reports whether there was one.
func (r *ServiceRepository) Delete(ctx context.Context, instanceID, name string) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM services WHERE instance_id = $1 AND name = $2`, instanceID, name)
	if err != nil {
		return false, fmt.Errorf("delete service: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

// ListByInstance returns a machine's services, by name.
func (r *ServiceRepository) ListByInstance(ctx context.Context, instanceID string) ([]*MachineService, error) {
	return r.list(ctx, `WHERE s.instance_id = $1 ORDER BY s.name`, instanceID)
}

// ListByOwner returns the services on all of the owner's machines.
func (r *ServiceRepository) ListByOwner(ctx context.Context, ownerID string) ([]*MachineService, error) {
	return r.list(ctx, `JOIN instances i ON i.id = s.instance_id WHERE i.owner_id = $1 ORDER BY s.instance_id, s.name`, ownerID)
}

func (r *ServiceRepository) list(ctx context.Context, where string, arg any) ([]*MachineService, error) {
	rows, err := r.db.Query(ctx,
		`SELECT s.instance_id, s.name, s.command, s.directory, COALESCE(s.port, 0), s.updated_at FROM services s `+where, arg)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	services, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*MachineService, error) {
		var s MachineService
		var updated time.Time
		if err := row.Scan(&s.InstanceID, &s.Name, &s.Command, &s.Directory, &s.Port, &updated); err != nil {
			return nil, err
		}
		s.Revision = updated.UnixMicro()
		return &s, nil
	})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	return services, nil
}
