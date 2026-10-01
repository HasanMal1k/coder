package guest

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

// forwardDialTimeout bounds connecting to a local port. Loopback connections
// succeed or are refused almost instantly, so this only guards the odd case.
const forwardDialTimeout = 5 * time.Second

func (s *Server) Forward(stream grpc.BidiStreamingServer[guestapi.ForwardRequest, guestapi.ForwardResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if open == nil {
		return status.Error(codes.InvalidArgument, "first message must be open")
	}
	port := open.GetPort()
	if port == 0 || port > 65535 {
		return status.Errorf(codes.InvalidArgument, "invalid port %d", port)
	}

	// "localhost" rather than 127.0.0.1, so programs that listen only on IPv6
	// loopback are reachable too.
	dialer := net.Dialer{Timeout: forwardDialTimeout}
	conn, err := dialer.DialContext(stream.Context(), "tcp", net.JoinHostPort("localhost", strconv.Itoa(int(port))))
	if err != nil {
		return status.Errorf(codes.Unavailable, "connect to port %d: %v", port, err)
	}
	defer conn.Close()

	err = stream.Send(&guestapi.ForwardResponse{
		Msg: &guestapi.ForwardResponse_Opened{Opened: &guestapi.ForwardOpened{}},
	})
	if err != nil {
		return err
	}

	// Client -> port. Ends when the client is done sending or goes away.
	go func() {
		for {
			in, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				// The client finished sending but may still be reading, so
				// close only our sending half.
				if tcp, ok := conn.(*net.TCPConn); ok {
					tcp.CloseWrite()
				}
				return
			}
			if err != nil {
				// The client is gone; closing unblocks the read loop below.
				conn.Close()
				return
			}

			if _, err := conn.Write(in.GetData()); err != nil {
				conn.Close()
				return
			}
		}
	}()

	// Port -> client. Returning ends the stream, which is how the client
	// learns the connection has closed.
	buf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			out := &guestapi.ForwardResponse{
				Msg: &guestapi.ForwardResponse_Data{Data: bytes.Clone(buf[:n])},
			}
			if err := stream.Send(out); err != nil {
				return err
			}
		}
		if err != nil {
			return nil
		}
	}
}
