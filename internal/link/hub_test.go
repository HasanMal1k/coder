package link

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/HasanMal1k/coder/internal/linkapi"
)

// pipe is a machine's end of a stream, for tests: what the hub sends arrives
// on sent, and what is put on incoming is what the machine sends.
type pipe struct {
	sent     chan *linkapi.Frame
	incoming chan *linkapi.Frame
	closed   chan struct{}
	once     sync.Once
	// sendErr, if set, is what Send fails with.
	sendErr error
}

func newPipe() *pipe {
	return &pipe{
		sent:     make(chan *linkapi.Frame, 256),
		incoming: make(chan *linkapi.Frame, 16),
		closed:   make(chan struct{}),
	}
}

func (p *pipe) Send(f *linkapi.Frame) error {
	if p.sendErr != nil {
		return p.sendErr
	}
	p.sent <- f
	return nil
}

func (p *pipe) Recv() (*linkapi.Frame, error) {
	select {
	case f := <-p.incoming:
		return f, nil
	case <-p.closed:
		return nil, io.EOF
	}
}

// close is the machine closing its side.
func (p *pipe) close() {
	p.once.Do(func() { close(p.closed) })
}

func ping(id uint64) *linkapi.Frame {
	return &linkapi.Frame{Id: id, Body: &linkapi.Frame_Ping{Ping: &linkapi.Ping{}}}
}

// serve runs Serve in the background and returns a channel with its result.
func serve(ctx context.Context, h *Hub, id string, p *pipe, handle func(*linkapi.Frame)) <-chan error {
	if handle == nil {
		handle = func(*linkapi.Frame) {}
	}

	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx, id, "linkd test", p, handle, nil) }()

	return done
}

func waitAttached(t *testing.T, h *Hub, id string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.Wait(ctx, id); err != nil {
		t.Fatalf("link %s never attached: %v", id, err)
	}
}

func result(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
		return nil
	}
}

func TestAttachedWhileServing(t *testing.T) {
	h := NewHub()
	p := newPipe()

	if h.Attached("a") {
		t.Fatal("attached before anything connected")
	}

	done := serve(context.Background(), h, "a", p, nil)
	waitAttached(t, h, "a")

	// The machine closes its side: that is a normal end.
	p.close()
	if err := result(t, done); err != nil {
		t.Fatalf("Serve returned %v, want nil", err)
	}
	if h.Attached("a") {
		t.Fatal("still attached after the stream ended")
	}
}

func TestSendReachesTheMachineInOrder(t *testing.T) {
	h := NewHub()
	p := newPipe()
	defer p.close()

	serve(context.Background(), h, "a", p, nil)
	waitAttached(t, h, "a")

	for id := uint64(1); id <= 5; id++ {
		if err := h.Send("a", ping(id)); err != nil {
			t.Fatalf("Send %d: %v", id, err)
		}
	}
	for want := uint64(1); want <= 5; want++ {
		select {
		case got := <-p.sent:
			if got.Id != want {
				t.Fatalf("frame %d arrived where %d was expected", got.Id, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("frame %d never arrived", want)
		}
	}
}

func TestFramesFromTheMachineAreHandledInOrder(t *testing.T) {
	h := NewHub()
	p := newPipe()
	defer p.close()

	got := make(chan uint64, 8)
	serve(context.Background(), h, "a", p, func(f *linkapi.Frame) { got <- f.Id })
	waitAttached(t, h, "a")

	for id := uint64(1); id <= 3; id++ {
		p.incoming <- ping(id)
	}
	for want := uint64(1); want <= 3; want++ {
		select {
		case id := <-got:
			if id != want {
				t.Fatalf("handled frame %d where %d was expected", id, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("frame %d was never handled", want)
		}
	}
}

func TestSendToAMachineThatIsNotAttached(t *testing.T) {
	h := NewHub()

	if err := h.Send("nobody", ping(1)); !errors.Is(err, ErrNotAttached) {
		t.Fatalf("Send returned %v, want ErrNotAttached", err)
	}
}

func TestSendDoesNotWaitForAMachineThatIsNotReading(t *testing.T) {
	h := NewHub()
	// Nothing reads what is sent to this machine, and Send to it blocks.
	p := newPipe()
	p.sent = make(chan *linkapi.Frame)
	defer p.close()

	serve(context.Background(), h, "a", p, nil)
	waitAttached(t, h, "a")

	// One frame is taken off the queue and is stuck being sent; the queue
	// then fills, and the next is refused, not waited for.
	refused := false
	for range backlog + 2 {
		if err := h.Send("a", ping(1)); err != nil {
			if !errors.Is(err, ErrBacklog) {
				t.Fatalf("Send returned %v, want ErrBacklog", err)
			}
			refused = true
			break
		}
	}
	if !refused {
		t.Fatal("a machine that reads nothing was sent frames without limit")
	}
}

func TestANewerLinkReplacesTheOld(t *testing.T) {
	h := NewHub()
	first, second := newPipe(), newPipe()
	defer first.close()
	defer second.close()

	firstDone := serve(context.Background(), h, "a", first, nil)
	waitAttached(t, h, "a")

	secondDone := serve(context.Background(), h, "a", second, nil)

	if err := result(t, firstDone); !errors.Is(err, ErrReplaced) {
		t.Fatalf("the first link's Serve returned %v, want ErrReplaced", err)
	}
	// The old link going away must not unregister its replacement.
	if !h.Attached("a") {
		t.Fatal("the replacement was unregistered along with the link it replaced")
	}

	if err := h.Send("a", ping(9)); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-second.sent:
		if f.Id != 9 {
			t.Fatalf("the newer link got frame %d", f.Id)
		}
	case <-first.sent:
		t.Fatal("a frame went to the link that was replaced")
	case <-time.After(2 * time.Second):
		t.Fatal("the frame never arrived")
	}

	second.close()
	if err := result(t, secondDone); err != nil {
		t.Fatalf("the second link's Serve returned %v", err)
	}
	if h.Attached("a") {
		t.Fatal("still attached after both links ended")
	}
}

func TestServeEndsWithItsContext(t *testing.T) {
	h := NewHub()
	p := newPipe()
	defer p.close()

	ctx, cancel := context.WithCancel(context.Background())
	done := serve(ctx, h, "a", p, nil)
	waitAttached(t, h, "a")

	cancel()
	if err := result(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Serve returned %v, want context.Canceled", err)
	}
	if h.Attached("a") {
		t.Fatal("still attached after its context ended")
	}
}

func TestServeEndsWhenSendingFails(t *testing.T) {
	h := NewHub()
	p := newPipe()
	p.sendErr = errors.New("connection reset")
	defer p.close()

	done := serve(context.Background(), h, "a", p, nil)
	waitAttached(t, h, "a")

	if err := h.Send("a", ping(1)); err != nil {
		t.Fatal(err)
	}
	if err := result(t, done); err == nil || err.Error() != "connection reset" {
		t.Fatalf("Serve returned %v, want the send's error", err)
	}
}

func TestWaitGivesUp(t *testing.T) {
	h := NewHub()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := h.Wait(ctx, "nobody"); !errors.Is(err, ErrNotAttached) {
		t.Fatalf("Wait returned %v, want ErrNotAttached", err)
	}
}

func TestWaitWakesWhenTheLinkAttaches(t *testing.T) {
	h := NewHub()
	p := newPipe()
	defer p.close()

	waited := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		waited <- h.Wait(ctx, "a")
	}()

	// Give the waiter time to start waiting before anything attaches.
	time.Sleep(20 * time.Millisecond)
	serve(context.Background(), h, "a", p, nil)

	if err := <-waited; err != nil {
		t.Fatalf("Wait returned %v", err)
	}
}
