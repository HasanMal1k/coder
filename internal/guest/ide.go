package guest

import (
	"context"
	"fmt"
	"log"
	"os/exec"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

// RunIDE keeps the editor (code-server) running until ctx is cancelled,
// restarting it with backoff if it exits. If the editor isn't installed it
// returns immediately.
//
// The editor listens on loopback only and does no authentication of its own:
// the only way in is through the tunnel, and the control plane decides who
// may use that.
func RunIDE(ctx context.Context) {
	path, err := exec.LookPath("code-server")
	if err != nil {
		log.Printf("code-server is not installed; the editor is unavailable")
		return
	}

	home := homeDir()

	supervise(ctx, "editor", func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, path,
			"--bind-addr", fmt.Sprintf("127.0.0.1:%d", guestapi.IDEPort),
			"--auth", "none",
			"--disable-telemetry",
			"--disable-update-check",
			"--disable-workspace-trust",
			home,
		)
		cmd.Env = childEnv()
		cmd.Dir = home

		return cmd
	})
}
