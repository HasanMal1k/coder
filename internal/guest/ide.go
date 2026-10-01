package guest

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

// ideStopTimeout is how long the editor gets to exit after being asked to.
const ideStopTimeout = 5 * time.Second

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

	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}

	backoff := minBackoff

	for {
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
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		// Ask politely first, so the editor can save its state.
		cmd.Cancel = func() error {
			return cmd.Process.Signal(syscall.SIGTERM)
		}
		cmd.WaitDelay = ideStopTimeout

		startedAt := time.Now()
		err := cmd.Run()
		if ctx.Err() != nil {
			return
		}
		log.Printf("editor exited: %v", err)

		if time.Since(startedAt) > stableAfter {
			backoff = minBackoff
		}

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		backoff = min(backoff*2, maxBackoff)
	}
}
