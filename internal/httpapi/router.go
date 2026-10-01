package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/HasanMal1k/coder/internal/auth"
	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/middleware"
	"github.com/HasanMal1k/coder/internal/portproxy"
	"github.com/HasanMal1k/coder/internal/wsgateway"
)

func NewRouter(authSvc *auth.Service, instanceSvc *instance.Service, gateway *wsgateway.Gateway, proxy *portproxy.Proxy, allowedOrigins []string) chi.Router {
	r := chi.NewRouter()

	// Lets a frontend served from another origin call the API. With no
	// allowed origins configured, only same-origin requests work.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		MaxAge:         300,
	}))

	authHandler := NewAuthHandler(authSvc)
	instanceHandler := NewInstanceHandler(instanceSvc, proxy)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	r.Post("/register", authHandler.Register)
	r.Post("/login", authHandler.Login)

	// WebSocket endpoints authenticate themselves: browsers can't send an
	// Authorization header on a WebSocket, and guests use their own token.
	r.Get("/instances/{id}/terminal", gateway.Terminal)
	r.Get("/guest/connect", gateway.GuestConnect)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(authSvc))
		r.Get("/me", authHandler.Me)

		r.Get("/instances", instanceHandler.List)
		r.Post("/instances", instanceHandler.Create)
		r.Get("/instances/{id}", instanceHandler.Get)
		r.Delete("/instances/{id}", instanceHandler.Delete)
		r.Post("/instances/{id}/start", instanceHandler.Start)
		r.Post("/instances/{id}/stop", instanceHandler.Stop)
		r.Post("/instances/{id}/ports/{port}/url", instanceHandler.PortURL)
	})

	return r
}
