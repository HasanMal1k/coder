package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/HasanMal1k/coder/internal/assistant"
	"github.com/HasanMal1k/coder/internal/auth"
	"github.com/HasanMal1k/coder/internal/guestapi"
	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/llmgateway"
	"github.com/HasanMal1k/coder/internal/middleware"
	"github.com/HasanMal1k/coder/internal/portproxy"
	"github.com/HasanMal1k/coder/internal/teamapi"
	"github.com/HasanMal1k/coder/internal/wsgateway"
)

// Deps is everything the router hands requests to.
type Deps struct {
	Auth      *auth.Service
	Instances *instance.Service
	Assistant *assistant.Service
	// AssistantModel is the name of the model the assistant uses.
	AssistantModel string
	Gateway        *wsgateway.Gateway
	Proxy          *portproxy.Proxy
	Models         *llmgateway.Gateway
	Team           *teamapi.Handler
	AllowedOrigins []string
}

func NewRouter(deps Deps) chi.Router {
	r := chi.NewRouter()

	// Lets a frontend served from another origin call the API. With no
	// allowed origins configured, only same-origin requests work.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: deps.AllowedOrigins,
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		MaxAge:         300,
	}))

	authHandler := NewAuthHandler(deps.Auth)
	instanceHandler := NewInstanceHandler(deps.Instances, deps.Proxy)
	chatHandler := NewChatHandler(deps.Assistant, deps.AssistantModel)
	agentHandler := NewAgentHandler(deps.Assistant)
	gateway := deps.Gateway

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	r.Post("/register", authHandler.Register)
	r.Post("/login", authHandler.Login)

	// WebSocket endpoints authenticate themselves: browsers can't send an
	// Authorization header on a WebSocket, and guests use their own token.
	r.Get("/instances/{id}/terminal", gateway.Terminal)
	r.Get("/presence", gateway.Presence)
	r.Get("/guest/connect", gateway.GuestConnect)

	// Machines call this, authenticating as themselves, for the model their
	// coding agent uses.
	r.Post(guestapi.ModelPath+"/chat/completions", deps.Models.ChatCompletions)

	// Machines also call these, for what their owner's other machines and
	// agents are doing: as files to keep a copy of, and as an MCP server
	// their coding agent asks questions of.
	r.Get(guestapi.TeamPath, deps.Team.Files)
	r.Post(guestapi.MCPPath, deps.Team.MCP)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(deps.Auth))
		r.Get("/me", authHandler.Me)
		r.Post("/token/renew", authHandler.Renew)

		r.Get("/instances", instanceHandler.List)
		r.Post("/instances", instanceHandler.Create)
		r.Get("/instances/{id}", instanceHandler.Get)
		r.Delete("/instances/{id}", instanceHandler.Delete)
		r.Post("/instances/{id}/start", instanceHandler.Start)
		r.Post("/instances/{id}/stop", instanceHandler.Stop)
		r.Post("/instances/{id}/ports/{port}/url", instanceHandler.PortURL)
		r.Post("/instances/{id}/ports/{port}/request", instanceHandler.Request)

		r.Get("/assistant", chatHandler.Info)
		r.Get("/chats", chatHandler.List)
		r.Post("/chats", chatHandler.Create)
		r.Get("/chats/{id}", chatHandler.Get)
		r.Delete("/chats/{id}", chatHandler.Delete)
		r.Post("/chats/{id}/messages", chatHandler.Send)
		r.Post("/chats/{id}/cancel", chatHandler.Cancel)
		r.Get("/chats/{id}/stream", chatHandler.Stream)
		r.Get("/chats/{id}/architecture", chatHandler.Architecture)
		r.Post("/chats/{id}/architecture/approve", chatHandler.ApproveArchitecture)

		r.Get("/agents", agentHandler.List)
		r.Post("/agents/{id}/stop", agentHandler.Stop)
	})

	return r
}
