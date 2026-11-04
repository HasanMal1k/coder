// Package linkgw is where machines' link daemons connect to the control
// plane: the gRPC server for the Link service. It checks which machine is
// calling, shakes hands with it, and hands its stream to the hub that keeps
// track of attached links.
package linkgw

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/link"
	"github.com/HasanMal1k/coder/internal/linkapi"
)

// handshakeTimeout bounds each step of the handshake: a daemon's hello, and
// its answer to the first ping. A variable so that tests need not wait.
var handshakeTimeout = 10 * time.Second

const (
	// maxEarlyFrames is how many frames a daemon may send before it has
	// answered the first ping.
	maxEarlyFrames = 16

	// name is what the control plane calls itself in its hello.
	name = "control plane"

	// A stream with nothing to say is pinged this often, and given this
	// long to answer, which is how a machine that vanished without closing
	// its connection gets noticed.
	keepaliveEvery   = 20 * time.Second
	keepaliveTimeout = 10 * time.Second
	// Daemons ping too, and may not do so more often than this.
	keepaliveMinFromClient = 10 * time.Second
)

// Guests checks a machine's credentials. *instance.Service implements it.
type Guests interface {
	AuthenticateGuest(ctx context.Context, id, token string) error
}

// Links is where attached links are kept. *link.Hub implements it.
type Links interface {
	Serve(ctx context.Context, instanceID, name string, stream link.Stream, handle func(*linkapi.Frame), attached func()) error
	Send(instanceID string, frame *linkapi.Frame) error
}

// Mesh is told what happens to machines' links, and decides what machines
// are told about each other. *assistant.Service implements it.
type Mesh interface {
	// LinkAttached is called once a machine's link can be sent to.
	LinkAttached(ctx context.Context, instanceID string)
	LinkDetached(instanceID string)
	// LinkStatus is what a machine has just reported of itself.
	LinkStatus(instanceID string, status *linkapi.Status)
}

// Server implements the Link service.
type Server struct {
	linkapi.UnimplementedLinkServer

	guests Guests
	links  Links
	mesh   Mesh
}

func New(guests Guests, links Links, mesh Mesh) *Server {
	return &Server{
		guests: guests,
		links:  links,
		mesh:   mesh,
	}
}

// GRPC returns a gRPC server serving s, set up for streams that stay open:
// it notices dead ones, lets daemons check on it in turn, and refuses frames
// over the protocol's limit.
func (s *Server) GRPC() *grpc.Server {
	srv := grpc.NewServer(
		grpc.MaxRecvMsgSize(linkapi.MaxFrame),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    keepaliveEvery,
			Timeout: keepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             keepaliveMinFromClient,
			PermitWithoutStream: true,
		}),
	)
	linkapi.RegisterLinkServer(srv, s)

	return srv
}

// Channel is one machine's link, for as long as it lasts.
func (s *Server) Channel(stream grpc.BidiStreamingServer[linkapi.Frame, linkapi.Frame]) error {
	ctx := stream.Context()

	instanceID, err := s.authenticate(ctx)
	if err != nil {
		return err
	}

	// Each side says what it is first.
	first, err := recvWithin(stream, handshakeTimeout)
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return status.Error(codes.FailedPrecondition, "the first frame must be hello")
	}
	if hello.Protocol != linkapi.Protocol {
		return status.Errorf(codes.FailedPrecondition, "this server speaks revision %d of the link protocol, and the daemon speaks %d", linkapi.Protocol, hello.Protocol)
	}
	err = stream.Send(&linkapi.Frame{Body: &linkapi.Frame_Hello{
		Hello: &linkapi.Hello{Protocol: linkapi.Protocol, Name: name},
	}})
	if err != nil {
		return err
	}

	// Only a link that actually answers is advertised.
	const firstPing = 1
	if err := stream.Send(&linkapi.Frame{Id: firstPing, Body: &linkapi.Frame_Ping{Ping: &linkapi.Ping{}}}); err != nil {
		return err
	}
	// The daemon may already have something to say, and its pong need not
	// be the first frame to arrive. What comes before it is kept, and dealt
	// with once the link is attached.
	var early []*linkapi.Frame
	for deadline := time.Now().Add(handshakeTimeout); ; {
		frame, err := recvWithin(stream, time.Until(deadline))
		if err != nil {
			return err
		}
		if frame.GetPong() != nil && frame.ReplyTo == firstPing {
			break
		}
		if len(early) == maxEarlyFrames {
			return status.Error(codes.FailedPrecondition, "the first ping was not answered")
		}
		early = append(early, frame)
	}

	handle := s.handler(instanceID)
	log.Printf("link attached: instance %s (%s)", instanceID, hello.Name)
	err = s.links.Serve(ctx, instanceID, hello.Name, stream, handle, func() {
		for _, frame := range early {
			handle(frame)
		}
		// On its own: it reads the database, and the stream must not wait.
		go s.mesh.LinkAttached(context.WithoutCancel(ctx), instanceID)
	})
	s.mesh.LinkDetached(instanceID)
	log.Printf("link detached: instance %s: %v", instanceID, err)

	switch {
	case err == nil:
		return nil
	case errors.Is(err, link.ErrReplaced):
		return status.Error(codes.Aborted, "replaced by a newer link from the same machine")
	case ctx.Err() != nil:
		// The daemon went away, or the server is shutting down.
		return status.FromContextError(ctx.Err()).Err()
	default:
		return err
	}
}

// handler deals with the frames an attached machine sends.
func (s *Server) handler(instanceID string) func(*linkapi.Frame) {
	return func(frame *linkapi.Frame) {
		switch body := frame.Body.(type) {
		case *linkapi.Frame_Status:
			s.mesh.LinkStatus(instanceID, body.Status)
		case *linkapi.Frame_Ping:
			// Best effort: a daemon whose ping goes unanswered asks again.
			s.links.Send(instanceID, &linkapi.Frame{
				ReplyTo: frame.Id,
				Body:    &linkapi.Frame_Pong{Pong: &linkapi.Pong{}},
			})
		}
		// Anything else is a kind this server does not know yet, and is
		// ignored, as the protocol asks.
	}
}

// authenticate works out which machine is calling, from the call's
// "authorization: Bearer <instance id>.<guest token>": the same credential a
// machine presents to the model gateway and the team API. It returns the
// instance's ID in the form the rest of the server uses.
func (s *Server) authenticate(ctx context.Context) (string, error) {
	var key string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get("authorization"); len(values) > 0 {
			key, _ = strings.CutPrefix(values[0], "Bearer ")
		}
	}
	rawID, token, _ := strings.Cut(key, ".")

	if err := s.guests.AuthenticateGuest(ctx, rawID, token); err != nil {
		if errors.Is(err, instance.ErrGuestUnauthorized) {
			return "", status.Error(codes.Unauthenticated, "invalid machine credentials")
		}
		log.Printf("linkgw: authenticate machine: %v", err)
		return "", status.Error(codes.Internal, "internal server error")
	}

	id, err := uuid.Parse(rawID)
	if err != nil {
		return "", status.Error(codes.Unauthenticated, "invalid machine credentials")
	}

	return id.String(), nil
}

// recvWithin receives one frame, giving up after d. Giving up ends the call,
// which is what stops the receive that was still waiting.
func recvWithin(stream link.Stream, d time.Duration) (*linkapi.Frame, error) {
	type received struct {
		frame *linkapi.Frame
		err   error
	}
	got := make(chan received, 1)
	go func() {
		frame, err := stream.Recv()
		got <- received{frame, err}
	}()

	select {
	case r := <-got:
		return r.frame, r.err
	case <-time.After(d):
		return nil, status.Error(codes.DeadlineExceeded, "the daemon took too long over the handshake")
	}
}
