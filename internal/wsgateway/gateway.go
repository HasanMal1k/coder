// Package wsgateway holds the WebSocket endpoints: the browser's terminal
// connection, and the tunnel each machine's guest daemon opens to us.
package wsgateway

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/HasanMal1k/coder/internal/auth"
	"github.com/HasanMal1k/coder/internal/guestapi"
	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/tunnel"
)

const (
	// helloTimeout is how long a browser has to send its auth message.
	helloTimeout = 10 * time.Second
	// pingInterval keeps idle terminals alive through proxies and notices
	// browsers that vanished without closing.
	pingInterval = 30 * time.Second
	pingTimeout  = 10 * time.Second
	// maxMessageBytes caps one message from a browser. A large paste arrives
	// as a single message, so this is generous.
	maxMessageBytes = 1 << 20
)

// Close codes sent to the browser, from the range WebSocket reserves for
// applications. They mirror the HTTP status the same failure would get.
const (
	CloseUnauthorized websocket.StatusCode = 4401
	CloseNotFound     websocket.StatusCode = 4404
	CloseNotRunning   websocket.StatusCode = 4409
	CloseUnreachable  websocket.StatusCode = 4503
)

// clientMessage is a text message from the browser. The first one must have
// type "auth" and carry the access token; later ones can be "resize".
// Keystrokes are sent as binary messages instead.
type clientMessage struct {
	Type  string `json:"type"`
	Token string `json:"token"`
	Cols  uint16 `json:"cols"`
	Rows  uint16 `json:"rows"`
}

type readyMessage struct {
	Type string `json:"type"`
}

type exitMessage struct {
	Type string `json:"type"`
	Code int    `json:"code"`
}

type Gateway struct {
	auth      *auth.Service
	instances *instance.Service
	// originPatterns are the browser origins allowed to open terminals from
	// a page served elsewhere.
	originPatterns []string
}

func New(authSvc *auth.Service, instanceSvc *instance.Service, allowedOrigins []string) *Gateway {
	return &Gateway{
		auth:           authSvc,
		instances:      instanceSvc,
		originPatterns: allowedOrigins,
	}
}

// Terminal connects a browser to a shell on one of the user's instances.
func (g *Gateway) Terminal(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: g.originPatterns})
	if err != nil {
		// Accept has already written the error response.
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(maxMessageBytes)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	hello, err := readHello(ctx, conn)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, "expected an auth message")
		return
	}

	user, err := g.auth.ParseJWT(ctx, hello.Token)
	if err != nil {
		conn.Close(CloseUnauthorized, "invalid or expired token")
		return
	}

	term, err := g.instances.OpenTerminal(ctx, user.ID, chi.URLParam(r, "id"), tunnel.Size{Cols: hello.Cols, Rows: hello.Rows})
	if err != nil {
		code, reason := closeFor(r, err)
		conn.Close(code, reason)
		return
	}

	if err := writeJSON(ctx, conn, readyMessage{Type: "ready"}); err != nil {
		term.Close()
		return
	}

	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		pumpOutput(ctx, conn, term)
	}()
	go keepAlive(ctx, conn)

	pumpInput(ctx, conn, term)

	// The browser is gone, or the shell has exited. Either way end the
	// terminal and let the output side finish before the connection goes.
	term.Close()
	<-outputDone
}

// GuestConnect accepts the tunnel a machine's guest daemon opens to us.
func (g *Gateway) GuestConnect(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get(guestapi.HeaderInstanceID)
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")

	if err := g.instances.AuthenticateGuest(r.Context(), id, token); err != nil {
		if errors.Is(err, instance.ErrGuestUnauthorized) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		log.Printf("authenticate guest: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	// The tunnel carries multiplexed frames of any size from a peer we have
	// just authenticated, so the per-message limit doesn't apply.
	conn.SetReadLimit(-1)

	log.Printf("guest connected: instance %s", id)
	err = g.instances.ServeGuest(r.Context(), id, websocket.NetConn(r.Context(), conn, websocket.MessageBinary))
	log.Printf("guest disconnected: instance %s: %v", id, err)
}

// readHello reads the auth message every terminal connection must start with.
func readHello(ctx context.Context, conn *websocket.Conn) (clientMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, helloTimeout)
	defer cancel()

	typ, data, err := conn.Read(ctx)
	if err != nil {
		return clientMessage{}, err
	}

	var msg clientMessage
	if typ != websocket.MessageText || json.Unmarshal(data, &msg) != nil || msg.Type != "auth" {
		return clientMessage{}, errors.New("first message is not an auth message")
	}

	return msg, nil
}

// pumpInput forwards the browser's keystrokes and resizes to the shell until
// the connection ends.
func pumpInput(ctx context.Context, conn *websocket.Conn, term *tunnel.Terminal) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		switch typ {
		case websocket.MessageBinary:
			if _, err := term.Write(data); err != nil {
				return
			}
		case websocket.MessageText:
			var msg clientMessage
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" {
				if err := term.Resize(tunnel.Size{Cols: msg.Cols, Rows: msg.Rows}); err != nil {
					return
				}
			}
		}
	}
}

// pumpOutput forwards the shell's output to the browser, then closes the
// connection with the reason the terminal ended.
func pumpOutput(ctx context.Context, conn *websocket.Conn, term *tunnel.Terminal) {
	for {
		data, err := term.Recv()
		if err != nil {
			var exit *tunnel.ExitError
			switch {
			case errors.As(err, &exit):
				writeJSON(ctx, conn, exitMessage{Type: "exit", Code: exit.Code})
				conn.Close(websocket.StatusNormalClosure, "shell exited")
			case ctx.Err() != nil:
				// The request is over (browser gone or server shutting
				// down); there is nobody left to tell.
			default:
				conn.Close(CloseUnreachable, "connection to the instance was lost")
			}
			return
		}

		if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
			return
		}
	}
}

func keepAlive(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				// Closing unblocks the read loop, which ends the terminal.
				conn.CloseNow()
				return
			}
		}
	}
}

func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return conn.Write(ctx, websocket.MessageText, data)
}

// closeFor picks the close code and reason for a failure to open a terminal.
func closeFor(r *http.Request, err error) (websocket.StatusCode, string) {
	switch {
	case errors.Is(err, instance.ErrNotFound):
		return CloseNotFound, "instance not found"
	case errors.Is(err, instance.ErrNotRunning):
		return CloseNotRunning, "instance is not running"
	case errors.Is(err, instance.ErrGuestOffline):
		return CloseUnreachable, "instance is not reachable yet"
	default:
		log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
		return websocket.StatusInternalError, "internal server error"
	}
}
