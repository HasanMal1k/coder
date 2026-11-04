package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// defaultIdleStopAfter is long enough to ride out a page reload or a brief
// loss of connection, and short enough that closing the browser stops
// machines promptly.
const defaultIdleStopAfter = 2 * time.Minute

type Config struct {
	DatabaseURL string
	JWTSecret   string
	Port        string
	// AccessTokenTTL is how long a signed-in user's token is valid for. Zero
	// means it never expires.
	AccessTokenTTL time.Duration
	// WorkspaceImage is the machine image new instances boot from.
	WorkspaceImage string
	// GuestConnectURL is where machines dial the control plane. It must be
	// reachable from inside a machine, which "localhost" is not.
	GuestConnectURL string
	// LinkAddr is where the server listens for machines' link daemons, which
	// speak gRPC: ":8081". LinkConnectURL is where machines dial it, and like
	// GuestConnectURL must be reachable from inside a machine.
	LinkAddr       string
	LinkConnectURL string
	// WorkspaceHost is the host that machine origins are subdomains of: a
	// machine's port is served at <port>-<instance id>.<WorkspaceHost>. It
	// should share a registrable domain with the frontend (app.example.com
	// and example.com, say), so that the two are one site to the browser.
	// WorkspaceScheme is how browsers reach it, "http" or "https".
	WorkspaceHost   string
	WorkspaceScheme string
	// IdleStopAfter is how long a running machine may go with nobody
	// connected (the owner's app closed, nothing using the machine) before
	// it is stopped. Zero turns this off.
	IdleStopAfter time.Duration
	// ModelAPIKey is the key for the model the assistant uses. Without one
	// the assistant is turned off. ModelBaseURL is the root of the model's
	// OpenAI-compatible API, and ModelName the model to ask for.
	ModelAPIKey  string
	ModelBaseURL string
	ModelName    string
	// ModelContextWindow and ModelMaxOutput are the model's own limits, in
	// tokens: how much a coding agent may hold before it summarises, and the
	// longest reply it may ask for. They should be the model's real ones, or
	// agents forget their work part-way through a task.
	ModelContextWindow int
	ModelMaxOutput     int
	// AgentReasoningEffort is how hard coding agents' model thinks: "low",
	// "high" or "max". Harder costs more output tokens.
	AgentReasoningEffort string
	// AllowedOrigins are the browser origins (e.g. "http://localhost:5173")
	// allowed to call the API and open terminals from another origin.
	AllowedOrigins []string
}

// Load reads configuration from the environment, loading a .env file first if
// one exists. Required values that are missing produce an error.
func Load() (Config, error) {
	// A missing .env is fine (e.g. production sets real env vars).
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}

	cfg := Config{
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		Port:            os.Getenv("PORT"),
		WorkspaceImage:  os.Getenv("WORKSPACE_IMAGE"),
		GuestConnectURL: os.Getenv("GUEST_CONNECT_URL"),
		LinkAddr:        os.Getenv("LINK_ADDR"),
		LinkConnectURL:  os.Getenv("LINK_CONNECT_URL"),
		WorkspaceHost:   os.Getenv("WORKSPACE_HOST"),
		WorkspaceScheme: os.Getenv("WORKSPACE_SCHEME"),
		ModelAPIKey:     os.Getenv("DEEPSEEK_API_KEY"),
		ModelBaseURL:    os.Getenv("LLM_BASE_URL"),
		ModelName:       os.Getenv("LLM_MODEL"),
		AllowedOrigins:  splitList(os.Getenv("CORS_ALLOWED_ORIGINS")),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.WorkspaceImage == "" {
		cfg.WorkspaceImage = "coder-workspace:dev"
	}
	if cfg.GuestConnectURL == "" {
		// Local Docker machines reach a control plane running on the host
		// through this name.
		cfg.GuestConnectURL = "ws://host.docker.internal:" + cfg.Port + "/guest/connect"
	}

	if cfg.LinkAddr == "" {
		cfg.LinkAddr = ":8081"
	}
	if cfg.LinkConnectURL == "" {
		_, port, err := net.SplitHostPort(cfg.LinkAddr)
		if err != nil {
			return Config{}, errors.New(`LINK_ADDR must be an address to listen on, such as ":8081"`)
		}
		// As with the tunnel: the name by which a local Docker machine
		// reaches a control plane running on the host.
		cfg.LinkConnectURL = "http://host.docker.internal:" + port
	}

	if cfg.WorkspaceHost == "" {
		// Browsers resolve every *.localhost name to this computer, so this
		// works in development with no DNS setup. It is "coder.localhost"
		// rather than plain "localhost" so that machines and the app (served
		// at coder.localhost:3000) belong to the same site: browsers restrict
		// cookies in a frame that is embedded from a different site.
		cfg.WorkspaceHost = "coder.localhost:" + cfg.Port
	}
	if cfg.WorkspaceScheme == "" {
		cfg.WorkspaceScheme = "http"
	}
	if cfg.WorkspaceScheme != "http" && cfg.WorkspaceScheme != "https" {
		return Config{}, errors.New("WORKSPACE_SCHEME must be http or https")
	}

	if cfg.ModelBaseURL == "" {
		cfg.ModelBaseURL = "https://api.deepseek.com/v1"
	}
	if cfg.ModelName == "" {
		cfg.ModelName = "deepseek-chat"
	}

	// DeepSeek V4's limits (api-docs.deepseek.com, pricing, October 2026).
	var err error
	if cfg.ModelContextWindow, err = tokens("LLM_CONTEXT_WINDOW", 1_000_000); err != nil {
		return Config{}, err
	}
	if cfg.ModelMaxOutput, err = tokens("LLM_MAX_OUTPUT", 384_000); err != nil {
		return Config{}, err
	}
	if cfg.ModelMaxOutput >= cfg.ModelContextWindow {
		return Config{}, errors.New("LLM_MAX_OUTPUT must be less than LLM_CONTEXT_WINDOW")
	}

	cfg.AgentReasoningEffort = os.Getenv("AGENT_REASONING_EFFORT")
	switch cfg.AgentReasoningEffort {
	case "":
		// What DeepSeek's own comparisons of coding agents were run at.
		cfg.AgentReasoningEffort = "max"
	case "low", "high", "max":
	default:
		return Config{}, errors.New(`AGENT_REASONING_EFFORT must be "low", "high" or "max"`)
	}

	if raw := os.Getenv("ACCESS_TOKEN_TTL"); raw != "" {
		ttl, err := time.ParseDuration(raw)
		if err != nil || ttl < 0 {
			return Config{}, errors.New(`ACCESS_TOKEN_TTL must be a duration such as "15m" or "24h", or "0" for tokens that never expire`)
		}
		cfg.AccessTokenTTL = ttl
	}

	cfg.IdleStopAfter = defaultIdleStopAfter
	if raw := os.Getenv("IDLE_STOP_AFTER"); raw != "" {
		after, err := time.ParseDuration(raw)
		if err != nil || after < 0 {
			return Config{}, errors.New(`IDLE_STOP_AFTER must be a duration such as "2m" or "90s", or "0" to turn it off`)
		}
		cfg.IdleStopAfter = after
	}

	return cfg, nil
}

// tokens reads a count of tokens from the environment, or gives the default.
func tokens(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a number of tokens, such as %d", name, fallback)
	}

	return n, nil
}

// splitList parses a comma-separated list, ignoring blanks.
func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}

	return out
}
