package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Chat struct {
	ID        string
	OwnerID   string
	Title     string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChatEvent is one entry in a chat's history. Payload is JSON whose shape
// depends on Kind; this package stores it without interpreting it.
type ChatEvent struct {
	ChatID    string
	Seq       int64
	Kind      string
	Payload   json.RawMessage
	CreatedAt time.Time
}

const chatColumns = `id, owner_id, title, status, created_at, updated_at`

type ChatRepository struct {
	db *pgxpool.Pool
}

func NewChatRepository(db *pgxpool.Pool) *ChatRepository {
	return &ChatRepository{
		db: db,
	}
}

// Create inserts chat, filling in its ID and timestamps.
func (r *ChatRepository) Create(ctx context.Context, chat *Chat) error {
	chat.ID = uuid.NewString()

	err := r.db.QueryRow(ctx,
		`INSERT INTO chats (id, owner_id, title, status)
		 VALUES ($1, $2, $3, $4)
		 RETURNING created_at, updated_at`,
		chat.ID, chat.OwnerID, chat.Title, chat.Status,
	).Scan(&chat.CreatedAt, &chat.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create chat: %w", err)
	}

	return nil
}

// GetByID returns the chat only if it belongs to ownerID.
func (r *ChatRepository) GetByID(ctx context.Context, id, ownerID string) (*Chat, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+chatColumns+`
		 FROM chats
		 WHERE id = $1 AND owner_id = $2`,
		id, ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("get chat: %w", err)
	}

	chat, err := pgx.CollectExactlyOneRow(rows, scanChat)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get chat: %w", err)
	}

	return chat, nil
}

// ListByOwner returns the owner's chats, most recently active first.
func (r *ChatRepository) ListByOwner(ctx context.Context, ownerID string) ([]*Chat, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+chatColumns+`
		 FROM chats
		 WHERE owner_id = $1
		 ORDER BY updated_at DESC`,
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list chats: %w", err)
	}

	chats, err := pgx.CollectRows(rows, scanChat)
	if err != nil {
		return nil, fmt.Errorf("list chats: %w", err)
	}

	return chats, nil
}

// TransitionStatus moves the chat to status to, but only if its current status
// is one of from, and reports whether it did. Like the same method on
// instances, the check and the change are one statement, so of two concurrent
// callers exactly one wins.
func (r *ChatRepository) TransitionStatus(ctx context.Context, id string, from []string, to string) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE chats
		 SET status = $3, updated_at = NOW()
		 WHERE id = $1 AND status = ANY($2)`,
		id, from, to,
	)
	if err != nil {
		return false, fmt.Errorf("transition chat status: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *ChatRepository) SetStatus(ctx context.Context, id, status string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE chats
		 SET status = $2, updated_at = NOW()
		 WHERE id = $1`,
		id, status,
	)
	if err != nil {
		return fmt.Errorf("set chat status: %w", err)
	}

	return nil
}

func (r *ChatRepository) SetTitle(ctx context.Context, id, title string) error {
	_, err := r.db.Exec(ctx, `UPDATE chats SET title = $2 WHERE id = $1`, id, title)
	if err != nil {
		return fmt.Errorf("set chat title: %w", err)
	}

	return nil
}

// ListByStatus returns every chat in the given status, whoever owns it. It is
// for background work, never for answering a user's request.
func (r *ChatRepository) ListByStatus(ctx context.Context, status string) ([]*Chat, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+chatColumns+`
		 FROM chats
		 WHERE status = $1`,
		status,
	)
	if err != nil {
		return nil, fmt.Errorf("list chats by status: %w", err)
	}

	chats, err := pgx.CollectRows(rows, scanChat)
	if err != nil {
		return nil, fmt.Errorf("list chats by status: %w", err)
	}

	return chats, nil
}

func (r *ChatRepository) Delete(ctx context.Context, id, ownerID string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM chats WHERE id = $1 AND owner_id = $2`, id, ownerID)
	if err != nil {
		return fmt.Errorf("delete chat: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// AppendEvent adds an event to the end of a chat's history and returns it
// with its sequence number. Callers must not append to the same chat
// concurrently: the next number is read and used in one statement, but two
// statements racing would pick the same one and the second would fail.
func (r *ChatRepository) AppendEvent(ctx context.Context, chatID, kind string, payload json.RawMessage) (*ChatEvent, error) {
	event := &ChatEvent{
		ChatID:  chatID,
		Kind:    kind,
		Payload: payload,
	}

	err := r.db.QueryRow(ctx,
		`INSERT INTO chat_events (chat_id, seq, kind, payload)
		 SELECT $1, COALESCE(MAX(seq), 0) + 1, $2, $3
		 FROM chat_events
		 WHERE chat_id = $1
		 RETURNING seq, created_at`,
		chatID, kind, payload,
	).Scan(&event.Seq, &event.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("append chat event: %w", err)
	}

	if _, err := r.db.Exec(ctx, `UPDATE chats SET updated_at = NOW() WHERE id = $1`, chatID); err != nil {
		return nil, fmt.Errorf("touch chat: %w", err)
	}

	return event, nil
}

// ListEvents returns a chat's events with a sequence number above after, in
// order.
func (r *ChatRepository) ListEvents(ctx context.Context, chatID string, after int64) ([]*ChatEvent, error) {
	rows, err := r.db.Query(ctx,
		`SELECT chat_id, seq, kind, payload, created_at
		 FROM chat_events
		 WHERE chat_id = $1 AND seq > $2
		 ORDER BY seq`,
		chatID, after,
	)
	if err != nil {
		return nil, fmt.Errorf("list chat events: %w", err)
	}

	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*ChatEvent, error) {
		var e ChatEvent
		if err := row.Scan(&e.ChatID, &e.Seq, &e.Kind, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		return &e, nil
	})
	if err != nil {
		return nil, fmt.Errorf("list chat events: %w", err)
	}

	return events, nil
}

func scanChat(row pgx.CollectableRow) (*Chat, error) {
	var c Chat
	if err := row.Scan(&c.ID, &c.OwnerID, &c.Title, &c.Status, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}
