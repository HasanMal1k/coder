package tunnel_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/HasanMal1k/coder/internal/guest"
	"github.com/HasanMal1k/coder/internal/tunnel"
)

// connect wires a Hub to a real guest server over an in-memory connection,
// the same way the two are joined by a WebSocket in production.
func connect(t *testing.T, hub *tunnel.Hub, instanceID string) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	hubSide, guestSide := net.Pipe()

	done := make(chan struct{}, 2)
	go func() {
		guest.ServeConn(ctx, guestSide, guest.NewServer("/bin/sh"))
		done <- struct{}{}
	}()
	go func() {
		hub.Serve(ctx, instanceID, hubSide)
		done <- struct{}{}
	}()

	t.Cleanup(func() {
		cancel()
		<-done
		<-done
	})
}

// waitOnline blocks until the hub has registered instanceID's guest.
func waitOnline(t *testing.T, hub *tunnel.Hub, instanceID string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for !hub.Online(instanceID) {
		if time.Now().After(deadline) {
			t.Fatal("guest never came online")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// readUntilExit collects a terminal's output until the shell exits.
func readUntilExit(t *testing.T, term *tunnel.Terminal) (string, int) {
	t.Helper()

	var out bytes.Buffer
	for {
		data, err := term.Recv()
		if err != nil {
			var exit *tunnel.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("terminal lost: %v (output so far: %q)", err, out.String())
			}
			return out.String(), exit.Code
		}
		out.Write(data)
	}
}

func TestTerminalRunsCommandsAndReportsExitCode(t *testing.T) {
	hub := tunnel.NewHub()
	connect(t, hub, "inst-1")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	term, err := hub.OpenTerminal(ctx, "inst-1", tunnel.Size{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	defer term.Close()

	// $((...)) proves the shell evaluated the line rather than the terminal
	// merely echoing what we typed.
	if _, err := term.Write([]byte("echo sum-$((40+2)); stty size; exit 7\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out, code := readUntilExit(t, term)
	if !strings.Contains(out, "sum-42") {
		t.Errorf("output %q does not contain the command's result", out)
	}
	if !strings.Contains(out, "30 100") {
		t.Errorf("output %q does not show the requested 30x100 size", out)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

func TestTerminalResize(t *testing.T) {
	hub := tunnel.NewHub()
	connect(t, hub, "inst-1")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	term, err := hub.OpenTerminal(ctx, "inst-1", tunnel.Size{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	defer term.Close()

	if err := term.Resize(tunnel.Size{Cols: 132, Rows: 43}); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	// Messages on a stream arrive in order, so the resize is applied before
	// this line runs.
	term.Write([]byte("stty size; exit\n"))

	out, _ := readUntilExit(t, term)
	if !strings.Contains(out, "43 132") {
		t.Errorf("output %q does not show the new 43x132 size", out)
	}
}

func TestTerminalExitsDespiteBackgroundJob(t *testing.T) {
	hub := tunnel.NewHub()
	connect(t, hub, "inst-1")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	term, err := hub.OpenTerminal(ctx, "inst-1", tunnel.Size{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	defer term.Close()

	// The background sleep keeps the terminal device open after the shell
	// exits; the exit must still be reported promptly.
	term.Write([]byte("sleep 5 & exit 0\n"))

	done := make(chan int, 1)
	go func() {
		_, code := readUntilExit(t, term)
		done <- code
	}()

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("exit was not reported while a background job held the terminal open")
	}
}

func TestOpenTerminalWaitsForGuest(t *testing.T) {
	hub := tunnel.NewHub()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// The guest connects shortly after the terminal is requested, as it does
	// when a machine has just been started.
	go func() {
		time.Sleep(300 * time.Millisecond)
		connect(t, hub, "inst-1")
	}()

	term, err := hub.OpenTerminal(ctx, "inst-1", tunnel.Size{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	defer term.Close()

	term.Write([]byte("exit 0\n"))
	if _, code := readUntilExit(t, term); code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestOpenTerminalOfflineGuest(t *testing.T) {
	hub := tunnel.NewHub()

	// A short deadline stands in for the hub's own wait period.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := hub.OpenTerminal(ctx, "nobody-home", tunnel.Size{Cols: 80, Rows: 24})
	if !errors.Is(err, tunnel.ErrGuestOffline) {
		t.Fatalf("err = %v, want ErrGuestOffline", err)
	}
}

func TestGuestGoesOfflineWhenConnectionDrops(t *testing.T) {
	hub := tunnel.NewHub()

	ctx, cancel := context.WithCancel(context.Background())
	hubSide, guestSide := net.Pipe()

	go guest.ServeConn(ctx, guestSide, guest.NewServer("/bin/sh"))
	served := make(chan struct{})
	go func() {
		hub.Serve(context.Background(), "inst-1", hubSide)
		close(served)
	}()

	waitOnline(t, hub, "inst-1")

	// Stopping the guest drops its end of the connection.
	cancel()

	select {
	case <-served:
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after the guest disconnected")
	}
	if hub.Online("inst-1") {
		t.Error("guest still reported online after disconnecting")
	}
}
