package guest

import (
	"context"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// stopTimeout is how long a supervised program gets to exit after being
// asked to.
const stopTimeout = 5 * time.Second

// supervise keeps a program running until ctx is cancelled, restarting it
// with backoff whenever it exits. command builds the program to run; it is
// called again for every restart.
func supervise(ctx context.Context, what string, command func(ctx context.Context) *exec.Cmd) {
	backoff := minBackoff

	for {
		cmd := command(ctx)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		// Ask politely first, so the program can save its state.
		cmd.Cancel = func() error {
			return cmd.Process.Signal(syscall.SIGTERM)
		}
		cmd.WaitDelay = stopTimeout

		startedAt := time.Now()
		err := cmd.Run()
		if ctx.Err() != nil {
			return
		}
		log.Printf("%s exited: %v", what, err)

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

// homeDir is the directory programs are started in.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/"
	}

	return home
}
