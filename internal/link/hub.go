// Package link is the control plane's end of the streams that machines' link
// daemons open to it. Each machine's linkd dials in; the Hub keeps track of
// which machines have a stream, and lets callers send frames down them. Like
// tunnel, it knows nothing about users or ownership: callers are expected to
// have authenticated the machine and authorized what they send.
package link

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/HasanMal1k/coder/internal/linkapi"
)

// backlog is how many frames may wait to be sent to one machine. A machine
// that has fallen this far behind is not sent more: nothing here buffers
// without bound.
const backlog = 64

var (
	// ErrNotAttached is returned when the machine has no link attached.
	ErrNotAttached = errors.New("the machine's link is not attached")
	// ErrBacklog is returned when a machine is not reading what it is sent
	// fast enough, and the frame was not queued.
	ErrBacklog = errors.New("the machine's link is not keeping up")
	// ErrReplaced is what Serve returns when the same machine attached
	// again: the newer stream wins.
	ErrReplaced = errors.New("replaced by a newer link from the same machine")
)

// Stream is a link daemon's stream, as the Hub needs it. The gRPC server
// stream satisfies it.
type Stream interface {
	Send(*linkapi.Frame) error
	Recv() (*linkapi.Frame, error)
}

// Hub is the set of machines with a link attached.
type Hub struct {
	mu    sync.Mutex
	links map[string]*attached
	// changed is closed and replaced every time a link attaches, waking
	// anyone waiting for one.
	changed chan struct{}
}

// attached is one machine's live link.
type attached struct {
	// name is what the daemon said it is, in its hello.
	name  string
	since time.Time
	// out is what is waiting to be sent to it.
	out chan *linkapi.Frame
	// replaced is closed when a newer link from the same machine takes over.
	replaced chan struct{}
}

func NewHub() *Hub {
	return &Hub{
		links:   make(map[string]*attached),
		changed: make(chan struct{}),
	}
}

// Serve registers stream as instanceID's link and blocks until the stream
// ends, ctx is cancelled, or the same machine attaches again. name is what
// the daemon called itself. Every frame the machine sends is passed to
// handle, one at a time and in order; handle must not block for long, since
// the machine's next frame waits for it. onAttach, if not nil, is called
// once the link is registered, from when on Send reaches it.
//
// The caller must have authenticated the machine and finished the handshake.
// Nothing else may send or receive on stream while Serve runs.
func (h *Hub) Serve(ctx context.Context, instanceID, name string, stream Stream, handle func(*linkapi.Frame), onAttach func()) error {
	a := &attached{
		name:     name,
		since:    time.Now(),
		out:      make(chan *linkapi.Frame, backlog),
		replaced: make(chan struct{}),
	}

	h.add(instanceID, a)
	defer h.remove(instanceID, a)

	if onAttach != nil {
		onAttach()
	}

	// Receiving has a goroutine of its own, because Recv can't be waited
	// on alongside anything else. It ends when the stream does, which is
	// at the latest when the caller returns.
	received := make(chan error, 1)
	go func() {
		for {
			frame, err := stream.Recv()
			if err != nil {
				received <- err
				return
			}
			handle(frame)
		}
	}()

	// Sending happens here and nowhere else: a stream takes one sender.
	for {
		select {
		case frame := <-a.out:
			if err := stream.Send(frame); err != nil {
				return err
			}
		case err := <-received:
			if errors.Is(err, io.EOF) {
				// The machine closed its side.
				return nil
			}
			return err
		case <-a.replaced:
			return ErrReplaced
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Send queues a frame for instanceID's link. It does not wait: a frame for a
// machine that is not attached, or that has fallen too far behind, is not
// sent, and the error says which. A frame that was queued may still be lost
// if the stream drops before it goes out; whoever needs to know a frame
// arrived asks for an answer to it.
func (h *Hub) Send(instanceID string, frame *linkapi.Frame) error {
	h.mu.Lock()
	a, ok := h.links[instanceID]
	h.mu.Unlock()

	if !ok {
		return ErrNotAttached
	}

	select {
	case a.out <- frame:
		return nil
	default:
		return ErrBacklog
	}
}

// Attached reports whether instanceID has a link attached right now.
func (h *Hub) Attached(instanceID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	_, ok := h.links[instanceID]
	return ok
}

// Wait blocks until instanceID has a link attached, or ctx ends. A machine
// that was started a moment ago needs time to boot and dial in.
func (h *Hub) Wait(ctx context.Context, instanceID string) error {
	for {
		h.mu.Lock()
		_, ok := h.links[instanceID]
		changed := h.changed
		h.mu.Unlock()

		if ok {
			return nil
		}

		select {
		case <-changed:
		case <-ctx.Done():
			return ErrNotAttached
		}
	}
}

func (h *Hub) add(instanceID string, a *attached) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if old, ok := h.links[instanceID]; ok {
		// Makes the old stream's Serve return.
		close(old.replaced)
	}
	h.links[instanceID] = a

	close(h.changed)
	h.changed = make(chan struct{})
}

// remove unregisters a, unless a newer link has already replaced it.
func (h *Hub) remove(instanceID string, a *attached) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.links[instanceID] == a {
		delete(h.links, instanceID)
	}
}
