package config

import (
	"strings"
	"testing"
)

// required sets what Load refuses to start without.
func required(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("JWT_SECRET", "secret")
}

func TestTheModelsLimitsAndEffort(t *testing.T) {
	required(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Unset, they are DeepSeek V4's, and agents think as hard as they can.
	if cfg.ModelContextWindow != 1_000_000 || cfg.ModelMaxOutput != 384_000 || cfg.AgentReasoningEffort != "max" {
		t.Errorf("defaults = %d, %d, %q", cfg.ModelContextWindow, cfg.ModelMaxOutput, cfg.AgentReasoningEffort)
	}

	t.Setenv("LLM_CONTEXT_WINDOW", "128000")
	t.Setenv("LLM_MAX_OUTPUT", "32000")
	t.Setenv("AGENT_REASONING_EFFORT", "high")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ModelContextWindow != 128000 || cfg.ModelMaxOutput != 32000 || cfg.AgentReasoningEffort != "high" {
		t.Errorf("set = %d, %d, %q", cfg.ModelContextWindow, cfg.ModelMaxOutput, cfg.AgentReasoningEffort)
	}
}

func TestBadModelSettingsAreRefused(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"LLM_CONTEXT_WINDOW", "a lot", "LLM_CONTEXT_WINDOW"},
		{"LLM_CONTEXT_WINDOW", "-5", "LLM_CONTEXT_WINDOW"},
		{"LLM_MAX_OUTPUT", "0", "LLM_MAX_OUTPUT"},
		{"LLM_MAX_OUTPUT", "2000000", "less than"},
		{"AGENT_REASONING_EFFORT", "medium", "AGENT_REASONING_EFFORT"},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			required(t)
			t.Setenv(tc.name, tc.value)

			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Load() error = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}
