package instance

import (
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
