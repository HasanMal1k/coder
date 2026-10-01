package tunnel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

// maxForwardChunk caps one message on a forwarded connection, well under
// gRPC's per-message limit.
const maxForwardChunk = 32 * 1024

var (
	// ErrPortClosed is returned when nothing on the machine is listening on
	// the requested port.
	ErrPortClosed = errors.New("nothing is listening on that port")
	// ErrUnsupported is returned when the machine's guest daemon is too old
	// to forward ports.
	ErrUnsupported = errors.New("the machine's guest daemon does not support port forwarding")
)

// DialPort opens a TCP connection to a port on instanceID's loopback
// interface. Unlike OpenTerminal it doesn't wait for a guest that isn't
// connected: callers are serving requests that shouldn't hang.
//
// The connection lives until it is closed; ctx only bounds establishing it.
func (h *Hub) DialPort(ctx context.Context, instanceID string, port int) (net.Conn, error) {
	h.mu.Lock()
	g, ok := h.guests[instanceID]
	h.mu.Unlock()
	if !ok {
		return nil, ErrGuestOffline
	}

	// The stream must outlive ctx, which is typically one request's context
	// while the connection may be pooled and reused by later requests.
	streamCtx, cancel := context.WithCancel(context.Background())

	// Until the connection is established, cancelling ctx abandons it.
	stop := context.AfterFunc(ctx, cancel)

	conn, err := openForward(streamCtx, g, port)
	if !stop() && err == nil {
		// ctx was cancelled just as the connection came up; nobody wants it.
		err = ctx.Err()
	}
	if err != nil {
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	conn.cancel = cancel

	return conn, nil
}

func openForward(ctx context.Context, g *guest, port int) (*portConn, error) {
	stream, err := g.client.Forward(ctx)
	if err != nil {
		return nil, fmt.Errorf("open forward: %w", err)
	}

	err = stream.Send(&guestapi.ForwardRequest{
		Msg: &guestapi.ForwardRequest_Open{Open: &guestapi.ForwardOpen{Port: uint32(port)}},
	})
	if err != nil {
		return nil, fmt.Errorf("open forward: %w", err)
	}

	// The guest answers only once it has connected, so a refused connection
	// is reported here rather than as a mysteriously dead connection later.
	if _, err := stream.Recv(); err != nil {
		switch status.Code(err) {
		case codes.Unavailable:
			return nil, ErrPortClosed
		case codes.Unimplemented:
			return nil, ErrUnsupported
		default:
			return nil, fmt.Errorf("open forward: %w", err)
		}
	}

	return &portConn{
		stream: stream,
		port:   port,
	}, nil
}

// portConn adapts a Forward stream to net.Conn, so anything that speaks TCP
// (such as net/http) can use a port on a machine. Like any net.Conn, one
// goroutine may read while another writes.
type portConn struct {
	stream grpc.BidiStreamingClient[guestapi.ForwardRequest, guestapi.ForwardResponse]
	cancel context.CancelFunc
	port   int

	// unread holds the rest of a message that didn't fit the last Read.
	unread []byte

	// sendMu serializes Write and CloseWrite: a gRPC stream allows only one
	// sender at a time.
	sendMu sync.Mutex
}

func (c *portConn) Read(p []byte) (int, error) {
	for len(c.unread) == 0 {
		msg, err := c.stream.Recv()
		if err != nil {
			// io.EOF passes through unchanged: the machine's side closed.
			return 0, err
		}
		c.unread = msg.GetData()
	}

	n := copy(p, c.unread)
	c.unread = c.unread[n:]

	return n, nil
}

func (c *portConn) Write(p []byte) (int, error) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	written := 0
	for len(p) > 0 {
		chunk := p[:min(len(p), maxForwardChunk)]

		// The caller may reuse p as soon as we return, and gRPC may still
		// hold the message, so it gets its own copy.
		err := c.stream.Send(&guestapi.ForwardRequest{
			Msg: &guestapi.ForwardRequest_Data{Data: bytes.Clone(chunk)},
		})
		if err != nil {
			return written, err
		}

		written += len(chunk)
		p = p[len(chunk):]
	}

	return written, nil
}

// CloseWrite signals that no more data will be written, while leaving the
// connection open for reading.
func (c *portConn) CloseWrite() error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	return c.stream.CloseSend()
}

func (c *portConn) Close() error {
	c.cancel()
	return nil
}

func (c *portConn) LocalAddr() net.Addr  { return forwardAddr{} }
func (c *portConn) RemoteAddr() net.Addr { return forwardAddr{port: c.port} }

// Deadlines are not supported. Nothing that uses these connections relies on
// them: requests are bounded by their contexts instead.
func (c *portConn) SetDeadline(time.Time) error      { return nil }
func (c *portConn) SetReadDeadline(time.Time) error  { return nil }
func (c *portConn) SetWriteDeadline(time.Time) error { return nil }

type forwardAddr struct {
	port int
}

func (forwardAddr) Network() string { return "tunnel" }

func (a forwardAddr) String() string {
	return fmt.Sprintf("guest:%d", a.port)
}
