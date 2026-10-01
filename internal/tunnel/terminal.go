package tunnel

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

// Size is a terminal's dimensions in character cells.
type Size struct {
	Cols uint16
	Rows uint16
}

func (s Size) proto() *guestapi.TerminalSize {
	return &guestapi.TerminalSize{
		Cols: uint32(s.Cols),
		Rows: uint32(s.Rows),
	}
}

// ExitError is returned by Terminal.Recv when the shell has exited. It is the
// normal end of a terminal, not a failure.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("shell exited with code %d", e.Code)
}

// Terminal is a shell running on a machine. Recv may be called from one
// goroutine while another calls Write and Resize.
type Terminal struct {
	stream grpc.BidiStreamingClient[guestapi.TerminalInput, guestapi.TerminalOutput]
	cancel context.CancelFunc

	// sendMu serializes Write and Resize: a gRPC stream allows only one
	// sender at a time.
	sendMu sync.Mutex
}

// Recv returns the next chunk of the shell's output. When the shell exits it
// returns an *ExitError; any other error means the terminal was lost.
func (t *Terminal) Recv() ([]byte, error) {
	for {
		out, err := t.stream.Recv()
		if err != nil {
			return nil, err
		}

		switch msg := out.GetMsg().(type) {
		case *guestapi.TerminalOutput_Data:
			return msg.Data, nil
		case *guestapi.TerminalOutput_ExitCode:
			return nil, &ExitError{Code: int(msg.ExitCode)}
		}
	}
}

// Write sends p to the shell's stdin.
func (t *Terminal) Write(p []byte) (int, error) {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()

	err := t.stream.Send(&guestapi.TerminalInput{
		Msg: &guestapi.TerminalInput_Data{Data: p},
	})
	if err != nil {
		return 0, err
	}

	return len(p), nil
}

func (t *Terminal) Resize(size Size) error {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()

	return t.stream.Send(&guestapi.TerminalInput{
		Msg: &guestapi.TerminalInput_Resize{Resize: size.proto()},
	})
}

// Close ends the terminal, hanging up the shell if it is still running.
func (t *Terminal) Close() error {
	t.cancel()
	return nil
}
