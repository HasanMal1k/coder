package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Architecture is the design of what a chat is building. Design is JSON this
// package stores without interpreting.
type Architecture struct {
	ChatID    string
	OwnerID   string
	Status    string
	Design    json.RawMessage
	UpdatedAt time.Time
}

type ArchitectureRepository struct {
	db *pgxpool.Pool
}

func NewArchitectureRepository(db *pgxpool.Pool) *ArchitectureRepository {
	return &ArchitectureRepository{
		db: db,
	}
}

// Save stores the chat's design, replacing any it had.
func (r *ArchitectureRepository) Save(ctx context.Context, a *Architecture) error {
	err := r.db.QueryRow(ctx,
		`INSERT INTO architectures (chat_id, owner_id, status, design)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (chat_id) DO UPDATE
		 SET status = EXCLUDED.status, design = EXCLUDED.design, updated_at = NOW()
		 RETURNING updated_at`,
		a.ChatID, a.OwnerID, a.Status, a.Design,
	).Scan(&a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save architecture: %w", err)
	}

	return nil
}

// Get returns the chat's design, or ErrNotFound if it has none.
func (r *ArchitectureRepository) Get(ctx context.Context, chatID string) (*Architecture, error) {
	var a Architecture

	err := r.db.QueryRow(ctx,
		`SELECT chat_id, owner_id, status, design, updated_at
		 FROM architectures
		 WHERE chat_id = $1`,
		chatID,
	).Scan(&a.ChatID, &a.OwnerID, &a.Status, &a.Design, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get architecture: %w", err)
	}

	return &a, nil
}

// ListApproved returns the owner's designs that have been agreed to, oldest
// first.
func (r *ArchitectureRepository) ListApproved(ctx context.Context, ownerID string) ([]*Architecture, error) {
	rows, err := r.db.Query(ctx,
		`SELECT chat_id, owner_id, status, design, updated_at
		 FROM architectures
		 WHERE owner_id = $1 AND status = 'approved'
		 ORDER BY created_at`,
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list architectures: %w", err)
	}

	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Architecture, error) {
		var a Architecture
		if err := row.Scan(&a.ChatID, &a.OwnerID, &a.Status, &a.Design, &a.UpdatedAt); err != nil {
			return nil, err
		}
		return &a, nil
	})
	if err != nil {
		return nil, fmt.Errorf("list architectures: %w", err)
	}

	return list, nil
}
