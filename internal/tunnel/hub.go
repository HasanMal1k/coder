// Package tunnel is the control plane's end of the connections machines open
// to it. Each machine's guestd dials in; the Hub keeps track of who is
// connected and lets callers open terminals on them. It knows nothing about
// users or ownership: callers are expected to have authorized the request.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

const (
	// pingTimeout bounds the check that a newly connected guest answers.
	pingTimeout = 10 * time.Second
	// guestWait is how long OpenTerminal waits for a machine's guest to
	// connect. A machine that was just started needs a moment to boot.
	guestWait = 30 * time.Second
)

// ErrGuestOffline is returned when the machine's guest is not connected and
// did not connect within the wait period.
var ErrGuestOffline = errors.New("guest is not connected")

type Hub struct {
	mu     sync.Mutex
	guests map[string]*guest
	// changed is closed and replaced every time a guest connects, waking
	// anyone waiting for one.
	changed chan struct{}
}

// guest is one machine's live connection.
type guest struct {
	session *yamux.Session
	client  guestapi.GuestClient
}

func NewHub() *Hub {
	return &Hub{
		guests:  make(map[string]*guest),
		changed: make(chan struct{}),
	}
}

// Serve registers conn as the tunnel to instanceID's guest and blocks until
// the connection drops or ctx is cancelled. The caller must have authenticated
// the guest already. If the instance already has a connection, the new one
// replaces it.
func (h *Hub) Serve(ctx context.Context, instanceID string, conn net.Conn) error {
	session, err := yamux.Client(conn, guestapi.MuxConfig())
	if err != nil {
		conn.Close()
		return fmt.Errorf("start session: %w", err)
	}
	defer session.Close()

	// gRPC normally dials an address. Here every "dial" opens a new stream on
	// the connection the guest made to us, so the target is just a label.
	grpcConn, err := grpc.NewClient("passthrough:///"+instanceID,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return session.Open()
		}),
	)
	if err != nil {
		return fmt.Errorf("create guest client: %w", err)
	}
	defer grpcConn.Close()

	g := &guest{
		session: session,
		client:  guestapi.NewGuestClient(grpcConn),
	}

	// Only advertise a guest that actually answers.
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	_, err = g.client.Ping(pingCtx, &guestapi.PingRequest{})
	cancel()
	if err != nil {
		return fmt.Errorf("ping guest: %w", err)
	}

	h.add(instanceID, g)
	defer h.remove(instanceID, g)

	select {
	case <-session.CloseChan():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Online reports whether instanceID's guest is connected right now.
func (h *Hub) Online(instanceID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	_, ok := h.guests[instanceID]
	return ok
}

// OpenTerminal starts a shell on instanceID's machine. If the guest isn't
// connected yet it waits a while for it. The terminal lives until Close is
// called or ctx is cancelled.
func (h *Hub) OpenTerminal(ctx context.Context, instanceID string, size Size) (*Terminal, error) {
	g, err := h.wait(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)

	stream, err := g.client.Terminal(ctx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open terminal: %w", err)
	}

	err = stream.Send(&guestapi.TerminalInput{
		Msg: &guestapi.TerminalInput_Start{
			Start: &guestapi.TerminalStart{Size: size.proto()},
		},
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start terminal: %w", err)
	}

	return &Terminal{
		stream: stream,
		cancel: cancel,
	}, nil
}

// wait returns instanceID's guest, waiting up to guestWait for it to connect.
func (h *Hub) wait(ctx context.Context, instanceID string) (*guest, error) {
	ctx, cancel := context.WithTimeout(ctx, guestWait)
	defer cancel()

	for {
		h.mu.Lock()
		g, ok := h.guests[instanceID]
		changed := h.changed
		h.mu.Unlock()

		if ok {
			return g, nil
		}

		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ErrGuestOffline
		}
	}
}

func (h *Hub) add(instanceID string, g *guest) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if old, ok := h.guests[instanceID]; ok {
		// Closing the old session makes its Serve call return.
		old.session.Close()
	}
	h.guests[instanceID] = g

	close(h.changed)
	h.changed = make(chan struct{})
}

// remove unregisters g, unless a newer connection has already replaced it.
func (h *Hub) remove(instanceID string, g *guest) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.guests[instanceID] == g {
		delete(h.guests, instanceID)
	}
}
