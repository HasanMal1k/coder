package linkgw

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/link"
	"github.com/HasanMal1k/coder/internal/linkapi"
)

const (
	machineID = "0b7a3c1e-5d2f-4e8a-9c6b-1f2e3d4c5b6a"
	token     = "the-right-token"
)

// guests knows one machine.
type guests struct {
	// broken makes every check fail as a database that is down would.
	broken bool
}

func (g guests) AuthenticateGuest(_ context.Context, id, tok string) error {
	if g.broken {
		return errors.New("database is down")
	}
	if id != machineID || tok != token {
		return instance.ErrGuestUnauthorized
	}

	return nil
}

// mesh records what the server tells it of links.
type mesh struct {
	mu       sync.Mutex
	attached int
	detached int
	linked   []string
}

func (m *mesh) LinkAttached(context.Context, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attached++
}

func (m *mesh) LinkDetached(string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.detached++
}

func (m *mesh) LinkStatus(_ string, status *linkapi.Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.linked = status.Linked
}

func (m *mesh) seen() (attached, detached int, linked []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.attached, m.detached, m.linked
}

// told is the mesh behind the server most recently started.
var told *mesh

// start runs the real server over an in-memory connection and returns a
// client for it, with the hub behind it.
func start(t *testing.T, g Guests) (linkapi.LinkClient, *link.Hub) {
	t.Helper()

	hub := link.NewHub()
	told = &mesh{}
	srv := New(g, hub, told).GRPC()

	lis := bufconn.Listen(1 << 20)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///link",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	return linkapi.NewLinkClient(conn), hub
}

type clientStream = grpc.BidiStreamingClient[linkapi.Frame, linkapi.Frame]

// open starts a call as the machine with the given credentials.
func open(t *testing.T, client linkapi.LinkClient, id, tok string) (clientStream, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+id+"."+tok)

	stream, err := client.Channel(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(cancel)

	return stream, cancel
}

func hello(protocol uint32) *linkapi.Frame {
	return &linkapi.Frame{Body: &linkapi.Frame_Hello{Hello: &linkapi.Hello{Protocol: protocol, Name: "linkd test"}}}
}

func pong(replyTo uint64) *linkapi.Frame {
	return &linkapi.Frame{ReplyTo: replyTo, Body: &linkapi.Frame_Pong{Pong: &linkapi.Pong{}}}
}

// attach goes through the whole handshake as a daemon does.
func attach(t *testing.T, client linkapi.LinkClient, hub *link.Hub) (clientStream, context.CancelFunc) {
	t.Helper()

	stream, cancel := open(t, client, machineID, token)
	if err := stream.Send(hello(linkapi.Protocol)); err != nil {
		t.Fatal(err)
	}

	back, err := stream.Recv()
	if err != nil {
		t.Fatalf("waiting for the server's hello: %v", err)
	}
	if back.GetHello() == nil || back.GetHello().Protocol != linkapi.Protocol {
		t.Fatalf("the server's first frame was %v, want its hello", back)
	}

	ping, err := stream.Recv()
	if err != nil {
		t.Fatalf("waiting for the server's ping: %v", err)
	}
	if ping.GetPing() == nil || ping.Id == 0 {
		t.Fatalf("the server's second frame was %v, want a ping with an id", ping)
	}
	if hub.Attached(machineID) {
		t.Fatal("the link was advertised before it had answered a ping")
	}
	if err := stream.Send(pong(ping.Id)); err != nil {
		t.Fatal(err)
	}

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	if err := hub.Wait(ctx, machineID); err != nil {
		t.Fatalf("the link never attached: %v", err)
	}

	return stream, cancel
}

// ended reads until the call ends and returns the status it ended with.
func ended(t *testing.T, stream clientStream) codes.Code {
	t.Helper()

	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			// The server ended the call without an error.
			return codes.OK
		}
		if err != nil {
			return status.Code(err)
		}
	}
}

func gone(t *testing.T, hub *link.Hub) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for hub.Attached(machineID) {
		if time.Now().After(deadline) {
			t.Fatal("the link is still attached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestADaemonAttachesAndIsAnswered(t *testing.T) {
	client, hub := start(t, guests{})
	stream, _ := attach(t, client, hub)

	// The daemon's own ping is answered, with its id.
	if err := stream.Send(&linkapi.Frame{Id: 7, Body: &linkapi.Frame_Ping{Ping: &linkapi.Ping{}}}); err != nil {
		t.Fatal(err)
	}
	answer, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if answer.GetPong() == nil || answer.ReplyTo != 7 {
		t.Fatalf("a ping with id 7 was answered with %v", answer)
	}

	// What the server sends through the hub arrives.
	if err := hub.Send(machineID, &linkapi.Frame{Id: 42, Body: &linkapi.Frame_Ping{Ping: &linkapi.Ping{}}}); err != nil {
		t.Fatal(err)
	}
	sent, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if sent.Id != 42 {
		t.Fatalf("the frame sent through the hub arrived as %v", sent)
	}

	// What it reports of itself reaches the mesh.
	report := &linkapi.Frame{Body: &linkapi.Frame_Status{Status: &linkapi.Status{Linked: []string{"peer-1"}}}}
	if err := stream.Send(report); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		attached, _, linked := told.seen()
		if attached == 1 && slices.Equal(linked, []string{"peer-1"}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the mesh was told of %d attachments and linked %v", attached, linked)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The daemon closing its side detaches it, cleanly.
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	if code := ended(t, stream); code != codes.OK {
		t.Fatalf("the call ended with %s, want OK", code)
	}
	gone(t, hub)
}

func TestWhatADaemonSaysBeforeItsPongIsKept(t *testing.T) {
	client, hub := start(t, guests{})

	stream, _ := open(t, client, machineID, token)
	stream.Send(hello(linkapi.Protocol))
	stream.Recv()
	ping, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}

	// Its report arrives ahead of its answer to the ping, as it can.
	stream.Send(&linkapi.Frame{Body: &linkapi.Frame_Status{Status: &linkapi.Status{Linked: []string{"peer-early"}}}})
	stream.Send(pong(ping.Id))

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	if err := hub.Wait(ctx, machineID); err != nil {
		t.Fatalf("the link never attached: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, _, linked := told.seen(); slices.Equal(linked, []string{"peer-early"}) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("what the daemon said before its pong was lost")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestADaemonThatNeverAnswersThePingIsDropped(t *testing.T) {
	client, hub := start(t, guests{})

	stream, _ := open(t, client, machineID, token)
	stream.Send(hello(linkapi.Protocol))
	for range maxEarlyFrames + 1 {
		stream.Send(&linkapi.Frame{Body: &linkapi.Frame_Status{Status: &linkapi.Status{}}})
	}

	if code := ended(t, stream); code != codes.FailedPrecondition {
		t.Fatalf("the call ended with %s, want FailedPrecondition", code)
	}
	if hub.Attached(machineID) {
		t.Fatal("it attached without answering the ping")
	}
}

func TestWrongCredentialsAreRefused(t *testing.T) {
	client, hub := start(t, guests{})

	for name, creds := range map[string][2]string{
		"wrong token":      {machineID, "not-the-token"},
		"unknown machine":  {"9f8e7d6c-5b4a-4c3d-8e2f-1a0b9c8d7e6f", token},
		"not a machine id": {"api", token},
		"nothing":          {"", ""},
	} {
		stream, _ := open(t, client, creds[0], creds[1])
		stream.Send(hello(linkapi.Protocol))

		if code := ended(t, stream); code != codes.Unauthenticated {
			t.Errorf("%s: the call ended with %s, want Unauthenticated", name, code)
		}
	}

	if hub.Attached(machineID) {
		t.Fatal("something attached")
	}
}

func TestAFailureToCheckCredentialsIsNotARefusal(t *testing.T) {
	client, _ := start(t, guests{broken: true})

	stream, _ := open(t, client, machineID, token)
	stream.Send(hello(linkapi.Protocol))

	// A daemon told "unauthenticated" might give up for good; this is the
	// server's trouble, and it should keep trying.
	if code := ended(t, stream); code != codes.Internal {
		t.Fatalf("the call ended with %s, want Internal", code)
	}
}

func TestTheFirstFrameMustBeHello(t *testing.T) {
	client, hub := start(t, guests{})

	stream, _ := open(t, client, machineID, token)
	stream.Send(pong(1))

	if code := ended(t, stream); code != codes.FailedPrecondition {
		t.Fatalf("the call ended with %s, want FailedPrecondition", code)
	}
	if hub.Attached(machineID) {
		t.Fatal("it attached without saying hello")
	}
}

func TestAnotherRevisionOfTheProtocolIsRefused(t *testing.T) {
	client, hub := start(t, guests{})

	stream, _ := open(t, client, machineID, token)
	stream.Send(hello(linkapi.Protocol + 1))

	_, err := stream.Recv()
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("the call ended with %v, want FailedPrecondition", err)
	}
	if hub.Attached(machineID) {
		t.Fatal("it attached speaking another revision")
	}
}

func TestADaemonThatDoesNotAnswerIsNotAdvertised(t *testing.T) {
	old := handshakeTimeout
	handshakeTimeout = 50 * time.Millisecond
	defer func() { handshakeTimeout = old }()

	client, hub := start(t, guests{})

	// It says hello, and then never answers the ping.
	stream, _ := open(t, client, machineID, token)
	stream.Send(hello(linkapi.Protocol))

	if code := ended(t, stream); code != codes.DeadlineExceeded {
		t.Fatalf("the call ended with %s, want DeadlineExceeded", code)
	}
	if hub.Attached(machineID) {
		t.Fatal("a link that never answered was advertised")
	}
}

func TestADaemonThatSaysNothingIsDropped(t *testing.T) {
	old := handshakeTimeout
	handshakeTimeout = 50 * time.Millisecond
	defer func() { handshakeTimeout = old }()

	client, _ := start(t, guests{})

	stream, _ := open(t, client, machineID, token)
	if code := ended(t, stream); code != codes.DeadlineExceeded {
		t.Fatalf("the call ended with %s, want DeadlineExceeded", code)
	}
}

func TestTheSameMachineAttachingAgainReplacesItsLink(t *testing.T) {
	client, hub := start(t, guests{})

	first, _ := attach(t, client, hub)

	// attach waits for a link to be attached, which the first already is,
	// so the second is known to have taken over only when the first ends.
	second, _ := open(t, client, machineID, token)
	second.Send(hello(linkapi.Protocol))
	second.Recv()
	ping, err := second.Recv()
	if err != nil {
		t.Fatal(err)
	}
	second.Send(pong(ping.Id))

	if code := ended(t, first); code != codes.Aborted {
		t.Fatalf("the first link ended with %s, want Aborted", code)
	}
	if !hub.Attached(machineID) {
		t.Fatal("the machine has no link after attaching a second time")
	}

	// Frames now go to the newer link.
	if err := hub.Send(machineID, &linkapi.Frame{Id: 5, Body: &linkapi.Frame_Ping{Ping: &linkapi.Ping{}}}); err != nil {
		t.Fatal(err)
	}
	got, err := second.Recv()
	if err != nil || got.Id != 5 {
		t.Fatalf("the newer link received %v, %v", got, err)
	}
}

func TestADaemonGoingAwayDetachesIt(t *testing.T) {
	client, hub := start(t, guests{})
	_, cancel := attach(t, client, hub)

	// As a machine that is stopped, or loses its network: no goodbye.
	cancel()
	gone(t, hub)
}

func TestAFrameOverTheLimitEndsTheCall(t *testing.T) {
	client, hub := start(t, guests{})
	stream, _ := attach(t, client, hub)

	huge := &linkapi.Frame{Body: &linkapi.Frame_Hello{Hello: &linkapi.Hello{Name: string(make([]byte, linkapi.MaxFrame+1))}}}
	if err := stream.Send(huge); err != nil {
		t.Fatal(err)
	}

	if code := ended(t, stream); code != codes.ResourceExhausted {
		t.Fatalf("the call ended with %s, want ResourceExhausted", code)
	}
	gone(t, hub)
}
