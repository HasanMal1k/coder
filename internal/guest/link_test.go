package guest

import (
	"slices"
	"strings"
	"testing"

	"github.com/HasanMal1k/coder/internal/guestapi"
	"github.com/HasanMal1k/coder/internal/linkapi"
)

func TestLinkDaemonIsGivenTheMachinesIdentity(t *testing.T) {
	// What the control plane sets on a machine, and so on guestd.
	t.Setenv(guestapi.EnvInstanceID, "0b7a3c1e-5d2f-4e8a-9c6b-1f2e3d4c5b6a")
	t.Setenv(guestapi.EnvGuestToken, "the-token")
	t.Setenv(linkapi.EnvLinkURL, "http://control:8081")

	cfg, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("a machine with no address for its tunnel was accepted")
	}
	t.Setenv(guestapi.EnvConnectURL, "ws://control:8080/guest/connect")
	if cfg, err = ConfigFromEnv(); err != nil {
		t.Fatal(err)
	}
	if cfg.LinkURL != "http://control:8081" {
		t.Fatalf("the link's address was read as %q", cfg.LinkURL)
	}

	env := linkEnv(cfg)
	for _, want := range []string{
		guestapi.EnvGuestToken + "=the-token",
		guestapi.EnvInstanceID + "=0b7a3c1e-5d2f-4e8a-9c6b-1f2e3d4c5b6a",
		linkapi.EnvLinkURL + "=http://control:8081",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("the daemon's environment lacks %s", want)
		}
	}

	// Once: a program reads the first or the last of a variable given
	// twice, depending on the program.
	tokens := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, guestapi.EnvGuestToken+"=") {
			tokens++
		}
	}
	if tokens != 1 {
		t.Errorf("the token is in the daemon's environment %d times, want once", tokens)
	}
}

func TestAMachineFromBeforeLinksHasNoAddressForOne(t *testing.T) {
	t.Setenv(guestapi.EnvInstanceID, "0b7a3c1e-5d2f-4e8a-9c6b-1f2e3d4c5b6a")
	t.Setenv(guestapi.EnvGuestToken, "the-token")
	t.Setenv(guestapi.EnvConnectURL, "ws://control:8080/guest/connect")
	t.Setenv(linkapi.EnvLinkURL, "")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("a machine without a link address must still start: %v", err)
	}
	if cfg.LinkURL != "" {
		t.Fatalf("the link's address was read as %q", cfg.LinkURL)
	}
}

func TestOtherProgramsAreStillNotGivenTheToken(t *testing.T) {
	t.Setenv(guestapi.EnvGuestToken, "the-token")

	for _, kv := range childEnv() {
		if strings.HasPrefix(kv, guestapi.EnvGuestToken+"=") {
			t.Fatal("the token is in the environment of the programs guestd starts")
		}
	}
}
