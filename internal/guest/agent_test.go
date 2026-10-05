package guest

import (
	"encoding/json"
	"testing"
)

func TestControlURL(t *testing.T) {
	for connect, want := range map[string]string{
		"ws://host.docker.internal:8080/guest/connect": "http://host.docker.internal:8080/llm/v1",
		"wss://api.example.com/guest/connect?x=1":      "https://api.example.com/llm/v1",
		"https://api.example.com/guest/connect":        "https://api.example.com/llm/v1",
	} {
		got, err := controlURL(connect, "/llm/v1")
		if err != nil || got != want {
			t.Errorf("controlURL(%q) = %q, %v; want %q", connect, got, err, want)
		}
	}

	for _, connect := range []string{"ftp://example.com/x", "://nonsense"} {
		if got, err := controlURL(connect, "/llm/v1"); err == nil {
			t.Errorf("controlURL(%q) = %q, want an error", connect, got)
		}
	}
}

func TestAgentConfig(t *testing.T) {
	raw, err := agentConfig(Config{
		ConnectURL: "ws://control:8080/guest/connect",
		InstanceID: "instance-id",
		Token:      "guest-token",
	})
	if err != nil {
		t.Fatalf("agentConfig: %v", err)
	}

	var config struct {
		Model            string   `json:"model"`
		EnabledProviders []string `json:"enabled_providers"`
		Permission       map[string]string
		MCP              map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			OAuth   *bool             `json:"oauth"`
			Headers map[string]string `json:"headers"`
		} `json:"mcp"`
		Provider map[string]struct {
			Options struct {
				BaseURL string `json:"baseURL"`
				APIKey  string `json:"apiKey"`
			} `json:"options"`
			Models map[string]any `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("the configuration is not valid JSON: %v", err)
	}

	provider := config.Provider["coder"]
	if provider.Options.BaseURL != "http://control:8080/llm/v1" {
		t.Errorf("baseURL = %q, want the control plane", provider.Options.BaseURL)
	}
	if provider.Options.APIKey != "instance-id.guest-token" {
		t.Errorf("apiKey = %q, want the machine's own identity", provider.Options.APIKey)
	}
	if config.Model != "coder/assistant" || provider.Models["assistant"] == nil {
		t.Errorf("model = %q with models %v; the default must be one the provider defines", config.Model, provider.Models)
	}
	if len(config.EnabledProviders) != 1 || config.EnabledProviders[0] != "coder" {
		t.Errorf("enabled_providers = %v; any other provider would be a model we don't control", config.EnabledProviders)
	}
	team := config.MCP["team"]
	if team.Type != "remote" || team.URL != "http://control:8080/mcp" {
		t.Errorf("mcp team = %+v, want the control plane's MCP server", team)
	}
	if team.Headers["Authorization"] != "Bearer instance-id.guest-token" {
		t.Errorf("mcp authorization = %q, want the machine's own identity", team.Headers["Authorization"])
	}
	if team.OAuth == nil || *team.OAuth {
		t.Error("mcp oauth is not switched off; the agent would try to sign in with a browser nobody has")
	}
	if config.Permission["*"] != "allow" || config.Permission["question"] != "deny" {
		t.Errorf("permission = %v; the agent must never wait for someone to approve or answer", config.Permission)
	}
}
