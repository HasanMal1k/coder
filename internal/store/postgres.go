package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool opens a connection pool and pings it, so a bad DATABASE_URL
// fails at startup rather than on the first request.
func NewPostgresPool(ctx context.Context, dbUrl string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dbUrl)
	if err != nil {
		return nil, fmt.Errorf("create db pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return pool, nil
}
