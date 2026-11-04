package instance

import (
	"errors"
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"a", true},
		{"dev", true},
		{"my-project-2", true},
		{"0day", true},
		{strings.Repeat("a", 40), true},

		{"", false},
		{strings.Repeat("a", 41), false},
		{"-leading", false},
		{"trailing-", false},
		{"-", false},
		{"Has-Caps", false},
		{"has space", false},
		{"under_score", false},
		{"dot.ted", false},
		{"slash/ed", false},
		{"new\nline", false},
		{"dev\n", false},
		{"ünïcode", false},
	}

	for _, tt := range tests {
		if got := validName.MatchString(tt.name); got != tt.want {
			t.Errorf("validName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSizeOf(t *testing.T) {
	tests := []struct {
		power string
		disk  int
		want  Size
	}{
		{"", 0, Size{CPUs: 2, MemoryMB: 2048, DiskGB: 10}},
		{"small", 0, Size{CPUs: 1, MemoryMB: 1024, DiskGB: 10}},
		{"medium", 20, Size{CPUs: 2, MemoryMB: 2048, DiskGB: 20}},
		{"large", 50, Size{CPUs: 4, MemoryMB: 4096, DiskGB: 50}},
	}
	for _, tt := range tests {
		got, err := SizeOf(tt.power, tt.disk)
		if err != nil || got != tt.want {
			t.Errorf("SizeOf(%q, %d) = %+v, %v; want %+v", tt.power, tt.disk, got, err, tt.want)
		}
		if !got.offered() {
			t.Errorf("SizeOf(%q, %d) returned a size that is not on offer", tt.power, tt.disk)
		}
	}

	for _, bad := range []struct {
		power string
		disk  int
	}{
		{"huge", 10}, {"Small", 10}, {"medium", 15}, {"medium", -10}, {"medium", 1000},
	} {
		if got, err := SizeOf(bad.power, bad.disk); !errors.Is(err, ErrInvalidSize) {
			t.Errorf("SizeOf(%q, %d) = %+v, %v; want ErrInvalidSize", bad.power, bad.disk, got, err)
		}
	}
}

func TestOnlyOfferedSizesAreAccepted(t *testing.T) {
	for _, size := range []Size{
		{CPUs: 64, MemoryMB: 2048, DiskGB: 10},
		{CPUs: 2, MemoryMB: 1024, DiskGB: 10}, // the CPUs of one level with the memory of another
		{CPUs: 2, MemoryMB: 2048, DiskGB: 11},
		{CPUs: 2, MemoryMB: 2048},
	} {
		if size.offered() {
			t.Errorf("%+v counts as on offer", size)
		}
	}
}

func TestStacks(t *testing.T) {
	for _, name := range []string{"", "base", "go", "cpp", "dotnet", "data"} {
		stack, err := StackOf(name)
		if err != nil || stack.Name == "" || stack.Has == "" {
			t.Errorf("StackOf(%q) = %+v, %v", name, stack, err)
		}
	}
	if stack, _ := StackOf(""); stack.Name != DefaultStack {
		t.Errorf("the default stack is %q, want %q", stack.Name, DefaultStack)
	}
	for _, name := range []string{"rust", "Go", "base "} {
		if _, err := StackOf(name); !errors.Is(err, ErrInvalidStack) {
			t.Errorf("StackOf(%q): %v, want ErrInvalidStack", name, err)
		}
	}
}

func TestImageFor(t *testing.T) {
	tests := []struct{ base, stack, want string }{
		{"coder-workspace:dev", "base", "coder-workspace:dev"},
		{"coder-workspace:dev", "go", "coder-workspace-go:dev"},
		{"coder-workspace", "cpp", "coder-workspace-cpp"},
		{"localhost:5000/coder-workspace", "dotnet", "localhost:5000/coder-workspace-dotnet"},
		{"registry.example.com:5000/team/workspace:v2", "data", "registry.example.com:5000/team/workspace-data:v2"},
	}
	for _, tt := range tests {
		if got := imageFor(tt.base, tt.stack); got != tt.want {
			t.Errorf("imageFor(%q, %q) = %q, want %q", tt.base, tt.stack, got, tt.want)
		}
	}
}

func TestMachineEnv(t *testing.T) {
	s := &Service{cfg: Config{
		GuestConnectURL: "ws://control/guest/connect",
		LinkConnectURL:  "http://control:8081",
		ModelContext:    1_000_000,
		ModelOutput:     384_000,
	}}

	env := s.machineEnv("id-1", "token-1")
	want := map[string]string{
		"CODER_INSTANCE_ID":   "id-1",
		"CODER_GUEST_TOKEN":   "token-1",
		"CODER_CONNECT_URL":   "ws://control/guest/connect",
		"CODER_LINK_URL":      "http://control:8081",
		"CODER_MODEL_CONTEXT": "1000000",
		"CODER_MODEL_OUTPUT":  "384000",
	}
	for name, value := range want {
		if env[name] != value {
			t.Errorf("%s = %q, want %q", name, env[name], value)
		}
	}

	// Limits that aren't known are left out, for the machine to fall back on.
	s.cfg.ModelContext, s.cfg.ModelOutput = 0, 0
	env = s.machineEnv("id-1", "token-1")
	if _, ok := env["CODER_MODEL_CONTEXT"]; ok {
		t.Errorf("env = %v; an unknown limit should not be set", env)
	}
}
