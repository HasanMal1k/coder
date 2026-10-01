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

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Organization string
	Subscription string
	DisplayName  string
	CreatedAt    time.Time
}

var (
	// ErrEmailTaken is returned when the email UNIQUE constraint rejects an insert.
	ErrEmailTaken = errors.New("email already registered")
	// ErrNotFound is returned when a lookup matches no row.
	ErrNotFound = errors.New("not found")
)

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

// CreateUser inserts a new user. passwordHash must already be bcrypt-hashed by the caller.
func (r *UserRepository) CreateUser(ctx context.Context, email, displayName, organization, subscription, passwordHash string) (*User, error) {
	u := &User{
		ID:           uuid.NewString(),
		Email:        email,
		DisplayName:  displayName,
		Organization: organization,
		Subscription: subscription,
		PasswordHash: passwordHash,
	}

	err := r.db.QueryRow(ctx,
		`INSERT INTO users (id, email, display_name, organization, subscription, password_hash)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING created_at`,
		u.ID, u.Email, u.DisplayName, u.Organization, u.Subscription, u.PasswordHash,
	).Scan(&u.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}

	return u, nil
}

// GetUserByEmail looks up a user by email, for login. It does not check the
// password — the caller compares the returned PasswordHash with bcrypt.
func (r *UserRepository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var u User

	err := r.db.QueryRow(ctx,
		`SELECT id, email, password_hash, organization, subscription, display_name, created_at
		 FROM users
		 WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Organization, &u.Subscription, &u.DisplayName, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get user by email: %w", err)
	}

	return &u, nil
}

func (r *UserRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
	var u User

	err := r.db.QueryRow(ctx,
		`SELECT id, email, password_hash, organization, subscription, display_name, created_at
	 From users
	 WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Organization, &u.Subscription, &u.DisplayName, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get user by id: %w", err)
	}

	return &u, nil

}
