package guest

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"google.golang.org/grpc"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

const (
	dialTimeout = 15 * time.Second
	minBackoff  = time.Second
	maxBackoff  = 30 * time.Second
	// stableAfter is how long a connection must last before the next
	// reconnect starts again from minBackoff.
	stableAfter = time.Minute
)

// Config says where the control plane is and who this machine is.
type Config struct {
	ConnectURL string
	InstanceID string
	Token      string
}

// ConfigFromEnv reads the variables the control plane sets on every machine.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		ConnectURL: os.Getenv(guestapi.EnvConnectURL),
		InstanceID: os.Getenv(guestapi.EnvInstanceID),
		Token:      os.Getenv(guestapi.EnvGuestToken),
	}

	if cfg.ConnectURL == "" {
		return Config{}, errors.New(guestapi.EnvConnectURL + " is required")
	}
	if cfg.InstanceID == "" {
		return Config{}, errors.New(guestapi.EnvInstanceID + " is required")
	}
	if cfg.Token == "" {
		return Config{}, errors.New(guestapi.EnvGuestToken + " is required")
	}

	return cfg, nil
}

// Run keeps a tunnel to the control plane open until ctx is cancelled,
// reconnecting with exponential backoff whenever it drops.
func Run(ctx context.Context, cfg Config, srv *Server) {
	backoff := minBackoff

	for {
		connectedAt := time.Now()
		err := connectAndServe(ctx, cfg, srv)
		if ctx.Err() != nil {
			return
		}
		log.Printf("control plane connection ended: %v", err)

		if time.Since(connectedAt) > stableAfter {
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

func connectAndServe(ctx context.Context, cfg Config, srv *Server) error {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+cfg.Token)
	header.Set(guestapi.HeaderInstanceID, cfg.InstanceID)

	ws, _, err := websocket.Dial(dialCtx, cfg.ConnectURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return fmt.Errorf("dial control plane: %w", err)
	}
	// The tunnel carries multiplexed frames of any size, so the per-message
	// limit meant for untrusted peers doesn't apply.
	ws.SetReadLimit(-1)

	log.Printf("connected to control plane")

	return ServeConn(ctx, websocket.NetConn(ctx, ws, websocket.MessageBinary), srv)
}

// ServeConn serves the guest API over conn until the connection drops or ctx
// is cancelled. The control plane opens streams on conn; each one carries
// gRPC.
func ServeConn(ctx context.Context, conn net.Conn, srv *Server) error {
	session, err := yamux.Server(conn, guestapi.MuxConfig())
	if err != nil {
		conn.Close()
		return fmt.Errorf("start session: %w", err)
	}
	defer session.Close()

	grpcServer := grpc.NewServer()
	guestapi.RegisterGuestServer(grpcServer, srv)
	// Stop, not GracefulStop: once the tunnel is gone nobody is listening to
	// the terminals on it.
	defer grpcServer.Stop()

	stop := context.AfterFunc(ctx, func() {
		session.Close()
	})
	defer stop()

	// A yamux session is a net.Listener: each stream the control plane opens
	// is accepted as a new connection.
	return grpcServer.Serve(session)
}
