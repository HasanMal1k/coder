package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/HasanMal1k/coder/internal/auth"
	"github.com/HasanMal1k/coder/internal/config"
	"github.com/HasanMal1k/coder/internal/httpapi"
	"github.com/HasanMal1k/coder/internal/store"
)

func main() {

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	pool, err := store.NewPostgresPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	log.Printf("connected to database")

	userRepo := store.NewUserRepository(pool)

	authSvc := auth.NewService(userRepo, []byte(cfg.JWTSecret), 15*time.Minute)

	router := httpapi.NewRouter(authSvc)

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatal(err)
	}

}
