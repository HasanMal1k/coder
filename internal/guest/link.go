package guest

import (
	"context"
	"log"
	"os/exec"

	"github.com/HasanMal1k/coder/internal/guestapi"
	"github.com/HasanMal1k/coder/internal/linkapi"
)

// RunLink keeps the link daemon (linkd) running until ctx is cancelled,
// restarting it with backoff if it exits. The daemon is this machine's end
// of the distributed harness: it holds a stream open to the control plane.
//
// It returns immediately on a machine that was created before links existed,
// which was never told where to dial, and on one whose image has no daemon.
func RunLink(ctx context.Context, cfg Config) {
	if cfg.LinkURL == "" {
		log.Printf("%s is not set: this machine was created before links, and runs none", linkapi.EnvLinkURL)
		return
	}

	path, err := exec.LookPath(linkapi.Program)
	if err != nil {
		log.Printf("%s is not installed; this machine takes no part in the harness", linkapi.Program)
		return
	}

	home := homeDir()

	supervise(ctx, "link daemon", func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, path)
		cmd.Env = linkEnv(cfg)
		cmd.Dir = home

		return cmd
	})
}

// linkEnv is the environment the link daemon is started with: guestd's own,
// which says which machine this is and where the control plane is. Unlike the
// shells and the programs a user's work runs in, the daemon is the machine
// speaking for itself, so it is given the machine's token as well.
func linkEnv(cfg Config) []string {
	return append(childEnv(), guestapi.EnvGuestToken+"="+cfg.Token)
}
