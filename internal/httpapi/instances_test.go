package httpapi

import "testing"

func TestTrimPartialRune(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain text is left alone", "tea", "tea"},
		{"whole characters are left alone", "thé", "thé"},
		{"half of a two-byte character goes", "th\xc3", "th"},
		{"two thirds of a three-byte character goes", "a\xe2\x82", "a"},
		{"a real replacement character stays", "a�", "a�"},
		{"nothing is nothing", "", ""},
	} {
		if got := string(trimPartialRune([]byte(tc.in))); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
