package httpapi

import (
	"github.com/HasanMal1k/coder/internal/auth"
	"github.com/HasanMal1k/coder/internal/middleware"
	"github.com/go-chi/chi/v5"
)

func NewRouter(authSvc *auth.Service) chi.Router {
	r := chi.NewRouter()

	authHandler := NewAuthHandler(authSvc)

	r.Post("/register", authHandler.Register)
	r.Post("/login", authHandler.Login)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(authSvc))
		r.Get("/me", authHandler.Me)
	})

	return r
}
