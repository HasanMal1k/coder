package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/HasanMal1k/coder/internal/auth/handler"
	"github.com/HasanMal1k/coder/internal/auth/repository"
	"github.com/HasanMal1k/coder/internal/auth/service"
	"github.com/HasanMal1k/coder/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newPool(ctx context.Context, dbUrl string) (*pgxpool.Pool, error) {

	pool, err := pgxpool.New(ctx, dbUrl)
	if err != nil {
		return nil, fmt.Errorf("create db pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return pool, nil

}

func main() {

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	pool, err := newPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	log.Printf("Connected to database: %s", cfg.DatabaseURL)

	// User repo
	userRepo := repository.NewUserRepository(pool)

	userSvc := service.NewUserService(userRepo, []byte(cfg.JWTSecret), 15*time.Minute)

	userHandler := handler.NewUserHandler(userSvc)

	router := userHandler.Routes()

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatal(err)
	}

}
