package guest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

// agentModel is the name the agent knows its model by. Which model that
// really is gets decided by the control plane, not here.
const agentModel = "assistant"

// The limits the agent works within when the control plane didn't say what
// the model's are: cautious, so that a long session is summarised before
// any model would refuse it.
const (
	fallbackContext = 64000
	fallbackOutput  = 8192
)

// RunAgent keeps the coding agent (OpenCode, as a server) running until ctx
// is cancelled, restarting it with backoff if it exits. If it isn't installed
// it returns immediately.
//
// The platform's assistant gives the agent its tasks. Like the editor, it
// listens on loopback only and is reached through the tunnel.
func RunAgent(ctx context.Context, cfg Config) {
	path, err := exec.LookPath("opencode")
	if err != nil {
		log.Printf("opencode is not installed; the assistant can't work on this machine")
		return
	}

	config, err := agentConfig(cfg)
	if err != nil {
		log.Printf("coding agent: %v", err)
		return
	}

	home := homeDir()

	supervise(ctx, "coding agent", func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, path, "serve",
			"--hostname", "127.0.0.1",
			"--port", strconv.Itoa(guestapi.AgentPort),
		)
		// Given in the environment, the configuration applies to this
		// server only. The user's own OpenCode settings, if they use it
		// from a terminal, are theirs.
		cmd.Env = append(childEnv(), "OPENCODE_CONFIG_CONTENT="+config)
		cmd.Dir = home

		return cmd
	})
}

// agentConfig is the agent's configuration, as JSON.
//
// Its only model provider is the control plane, which forwards to the real
// model: the provider's API key never reaches a machine. What the agent
// presents instead is this machine's own identity.
func agentConfig(cfg Config) (string, error) {
	modelURL, err := controlURL(cfg.ConnectURL, guestapi.ModelPath)
	if err != nil {
		return "", err
	}
	teamURL, err := controlURL(cfg.ConnectURL, guestapi.MCPPath)
	if err != nil {
		return "", err
	}

	// This machine's own identity, which is all it ever presents to the
	// control plane.
	identity := cfg.InstanceID + "." + cfg.Token

	const provider = "coder"
	model := provider + "/" + agentModel

	// The model's own limits. With less room than the model has, the agent
	// summarises its session part-way through a task and forgets its work.
	window, output := cfg.ModelContext, cfg.ModelOutput
	if window <= 0 || output <= 0 || output >= window {
		window, output = fallbackContext, fallbackOutput
	}

	config := map[string]any{
		"$schema":     "https://opencode.ai/config.json",
		"model":       model,
		"small_model": model,
		"autoupdate":  false,
		"share":       "disabled",
		// Without this the agent would fall back to other providers it
		// knows of, some of them hosted services.
		"enabled_providers": []string{provider},
		// Nobody is there to approve anything, and the machine is the
		// user's to change as they like. It can't ask questions either:
		// those go through the assistant.
		"permission": map[string]string{
			"*":                  "allow",
			"external_directory": "allow",
			"doom_loop":          "allow",
			"question":           "deny",
		},
		"provider": map[string]any{
			provider: map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "coder",
				"options": map[string]string{
					"baseURL": modelURL,
					"apiKey":  identity,
				},
				"models": map[string]any{
					agentModel: map[string]any{
						"name":  "Assistant",
						"limit": map[string]int{"context": window, "output": output},
					},
				},
			},
		},
	}

	// The team: an MCP server on the control plane through which the agent
	// can ask about the owner's other machines and agents. Signing in with
	// a browser is switched off: the header is the whole of it.
	config["mcp"] = map[string]any{
		"team": map[string]any{
			"type":    "remote",
			"url":     teamURL,
			"enabled": true,
			"oauth":   false,
			"headers": map[string]string{"Authorization": "Bearer " + identity},
			// Long, because some of its tools wait for something to
			// happen: a service to come up, another agent to answer.
			"timeout": 600000,
		},
	}

	raw, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("encode configuration: %w", err)
	}

	return string(raw), nil
}

// controlURL is the address of a path on the control plane: the same host
// this machine dials for its tunnel, over HTTP.
func controlURL(connectURL, path string) (string, error) {
	u, err := url.Parse(connectURL)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", guestapi.EnvConnectURL, err)
	}

	switch strings.ToLower(u.Scheme) {
	case "ws", "http":
		u.Scheme = "http"
	case "wss", "https":
		u.Scheme = "https"
	default:
		return "", fmt.Errorf("%s has scheme %q, want ws or wss", guestapi.EnvConnectURL, u.Scheme)
	}

	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""

	return u.String(), nil
}
