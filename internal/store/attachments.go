package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Attachment is a file put in a chat, by its owner or by one of its agents.
type Attachment struct {
	ID          string
	OwnerID     string
	ChatID      string
	Name        string
	ContentType string
	Size        int
	// Data is the file itself. It is read only by Get; listings leave it
	// out.
	Data      []byte
	CreatedAt time.Time
}

type AttachmentRepository struct {
	db *pgxpool.Pool
}

func NewAttachmentRepository(db *pgxpool.Pool) *AttachmentRepository {
	return &AttachmentRepository{
		db: db,
	}
}

// Create stores a, filling in its ID, size and creation time.
func (r *AttachmentRepository) Create(ctx context.Context, a *Attachment) error {
	a.ID = uuid.NewString()
	a.Size = len(a.Data)

	err := r.db.QueryRow(ctx,
		`INSERT INTO attachments (id, owner_id, chat_id, name, content_type, size, data)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING created_at`,
		a.ID, a.OwnerID, a.ChatID, a.Name, a.ContentType, a.Size, a.Data,
	).Scan(&a.CreatedAt)
	if err != nil {
		return fmt.Errorf("create attachment: %w", err)
	}

	return nil
}

// Get returns one of a chat's attachments, with its data, only if it belongs
// to ownerID.
func (r *AttachmentRepository) Get(ctx context.Context, id, chatID, ownerID string) (*Attachment, error) {
	var a Attachment
	err := r.db.QueryRow(ctx,
		`SELECT id, owner_id, chat_id, name, content_type, size, data, created_at
		 FROM attachments
		 WHERE id = $1 AND chat_id = $2 AND owner_id = $3`,
		id, chatID, ownerID,
	).Scan(&a.ID, &a.OwnerID, &a.ChatID, &a.Name, &a.ContentType, &a.Size, &a.Data, &a.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get attachment: %w", err)
	}

	return &a, nil
}
