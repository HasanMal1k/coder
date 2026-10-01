package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	JWTSecret   string
	Port        string
	// WorkspaceImage is the machine image new instances boot from.
	WorkspaceImage string
	// GuestConnectURL is where machines dial the control plane. It must be
	// reachable from inside a machine, which "localhost" is not.
	GuestConnectURL string
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

	return cfg, nil
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
