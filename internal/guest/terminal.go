// Package guest is the code that runs inside a user's machine: it serves the
// guest API (terminals) and keeps a tunnel open to the control plane.
package guest

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

const (
	// drainTimeout is how long we keep forwarding output after the shell
	// exits. A background job can hold the terminal open indefinitely, so we
	// can't simply wait for end-of-file.
	drainTimeout = 200 * time.Millisecond
	// killTimeout is how long a shell gets to exit after a hangup before it
	// is killed.
	killTimeout = 3 * time.Second
)

// Server implements the guest API.
type Server struct {
	guestapi.UnimplementedGuestServer

	// shell is the command started for each terminal.
	shell []string
}

// NewServer returns a Server that runs shell for each terminal. With no
// arguments it uses a bash login shell.
func NewServer(shell ...string) *Server {
	if len(shell) == 0 {
		shell = []string{"/bin/bash", "-l"}
	}

	return &Server{
		shell: shell,
	}
}

func (s *Server) Ping(ctx context.Context, _ *guestapi.PingRequest) (*guestapi.PingResponse, error) {
	return &guestapi.PingResponse{}, nil
}

func (s *Server) Terminal(stream grpc.BidiStreamingServer[guestapi.TerminalInput, guestapi.TerminalOutput]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	start := first.GetStart()
	if start == nil {
		return status.Error(codes.InvalidArgument, "first message must be start")
	}

	cmd := exec.Command(s.shell[0], s.shell[1:]...)
	cmd.Env = shellEnv()
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}

	// StartWithSize makes the shell the leader of a new session with the PTY
	// as its controlling terminal, which is what makes job control and
	// Ctrl+C work.
	size := winsize(start.GetSize())
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: size.Col, Rows: size.Row})
	if err != nil {
		return status.Errorf(codes.Internal, "start shell: %v", err)
	}
	defer ptmx.Close()

	// The pty package leaves the terminal in blocking mode, where closing it
	// can't interrupt a read in progress. Our shutdown below depends on that.
	if err := control(ptmx, func(fd int) error { return syscall.SetNonblock(fd, true) }); err != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Wait()
		return status.Errorf(codes.Internal, "configure terminal: %v", err)
	}

	// Shell output -> client. This is the only goroutine that sends on the
	// stream until it finishes.
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)

		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				out := &guestapi.TerminalOutput{
					Msg: &guestapi.TerminalOutput_Data{Data: bytes.Clone(buf[:n])},
				}
				if stream.Send(out) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Client input -> shell. Ends only when the client goes away; a write
	// failing because the shell just exited is not a reason to hang up.
	clientGone := make(chan struct{})
	go func() {
		defer close(clientGone)

		for {
			in, err := stream.Recv()
			if err != nil {
				return
			}

			switch msg := in.GetMsg().(type) {
			case *guestapi.TerminalInput_Data:
				ptmx.Write(msg.Data)
			case *guestapi.TerminalInput_Resize:
				control(ptmx, func(fd int) error {
					return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, winsize(msg.Resize))
				})
			}
		}
	}()

	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
	}()

	select {
	case err := <-exited:
		select {
		case <-outputDone:
		case <-time.After(drainTimeout):
		}
		ptmx.Close()
		<-outputDone

		return stream.Send(&guestapi.TerminalOutput{
			Msg: &guestapi.TerminalOutput_ExitCode{ExitCode: exitCode(err)},
		})

	case <-clientGone:
		// The user closed the terminal: hang up, as closing a real terminal
		// window would.
		syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
		ptmx.Close()

		select {
		case <-exited:
		case <-time.After(killTimeout):
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-exited
		}
		<-outputDone

		return nil
	}
}

// shellEnv is the environment given to shells: guestd's own, minus its
// credential, with a terminal type programs understand.
func shellEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, guestapi.EnvGuestToken+"=") || strings.HasPrefix(kv, "TERM=") {
			continue
		}
		env = append(env, kv)
	}

	return append(env, "TERM=xterm-256color")
}

// control runs fn on f's file descriptor without changing its blocking mode,
// which calling f.Fd() would do.
func control(f *os.File, fn func(fd int) error) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}

	var fnErr error
	if err := conn.Control(func(fd uintptr) { fnErr = fn(int(fd)) }); err != nil {
		return err
	}

	return fnErr
}

func winsize(size *guestapi.TerminalSize) *unix.Winsize {
	cols, rows := size.GetCols(), size.GetRows()
	if cols == 0 || cols > 1000 {
		cols = 80
	}
	if rows == 0 || rows > 1000 {
		rows = 24
	}

	return &unix.Winsize{
		Col: uint16(cols),
		Row: uint16(rows),
	}
}

// exitCode converts the result of cmd.Wait into a shell-style exit code,
// using 128+signal for a process killed by a signal.
func exitCode(err error) int32 {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int32(ws.Signal())
		}
		return int32(exitErr.ExitCode())
	}

	return -1
}
