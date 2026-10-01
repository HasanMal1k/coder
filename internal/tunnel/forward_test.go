package tunnel_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HasanMal1k/coder/internal/tunnel"
)

// portOf returns the TCP port a listener or test server is bound to.
func portOf(t *testing.T, addr net.Addr) int {
	t.Helper()
	return addr.(*net.TCPAddr).Port
}

func TestDialPortCarriesHTTP(t *testing.T) {
	// Stands in for a program running on the machine. The in-process guest
	// dials this test's own loopback interface.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Echo-Method", r.Method)
		w.Write(body)
	}))
	defer backend.Close()
	port := portOf(t, backend.Listener.Addr())

	hub := tunnel.NewHub()
	connect(t, hub, "inst-1")
	waitOnline(t, hub, "inst-1")

	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return hub.DialPort(ctx, "inst-1", port)
			},
		},
	}
	defer client.CloseIdleConnections()

	// Larger than any buffer along the way, so it must be chunked, flow
	// controlled and reassembled in order.
	payload := make([]byte, 3<<20)
	rand.Read(payload)

	for i := range 3 {
		resp, err := client.Post("http://machine/echo", "application/octet-stream", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		got, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("request %d: read body: %v", i, err)
		}

		if resp.Header.Get("X-Echo-Method") != "POST" {
			t.Errorf("request %d: backend did not see a POST", i)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("request %d: body came back different (%d bytes, want %d)", i, len(got), len(payload))
		}
	}
}

func TestDialPortHalfClose(t *testing.T) {
	// Replies only after it has seen the client finish sending, so this
	// passes only if closing our write side reaches the machine while our
	// read side stays open.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		data, _ := io.ReadAll(conn)
		fmt.Fprintf(conn, "received %d bytes", len(data))
	}()

	hub := tunnel.NewHub()
	connect(t, hub, "inst-1")
	waitOnline(t, hub, "inst-1")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := hub.DialPort(ctx, "inst-1", portOf(t, listener.Addr()))
	if err != nil {
		t.Fatalf("DialPort: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("hello, machine")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := conn.(interface{ CloseWrite() error }).CloseWrite(); err != nil {
		t.Fatalf("CloseWrite: %v", err)
	}

	reply, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(reply) != "received 14 bytes" {
		t.Errorf("reply = %q, want %q", reply, "received 14 bytes")
	}
}

func TestDialPortNothingListening(t *testing.T) {
	// Bind a port and release it, to get one that is certainly closed.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := portOf(t, listener.Addr())
	listener.Close()

	hub := tunnel.NewHub()
	connect(t, hub, "inst-1")
	waitOnline(t, hub, "inst-1")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err = hub.DialPort(ctx, "inst-1", port)
	if !errors.Is(err, tunnel.ErrPortClosed) {
		t.Fatalf("err = %v, want ErrPortClosed", err)
	}
}

func TestDialPortOfflineGuest(t *testing.T) {
	hub := tunnel.NewHub()

	start := time.Now()
	_, err := hub.DialPort(context.Background(), "nobody-home", 8080)
	if !errors.Is(err, tunnel.ErrGuestOffline) {
		t.Fatalf("err = %v, want ErrGuestOffline", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v; an offline guest should be reported immediately", elapsed)
	}
}

func TestDialPortConnectionOutlivesDialContext(t *testing.T) {
	// HTTP clients dial with one request's context and then keep the
	// connection for later requests, so it must survive that context ending.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	hub := tunnel.NewHub()
	connect(t, hub, "inst-1")
	waitOnline(t, hub, "inst-1")

	dialCtx, cancel := context.WithCancel(context.Background())
	conn, err := hub.DialPort(dialCtx, "inst-1", portOf(t, listener.Addr()))
	if err != nil {
		t.Fatalf("DialPort: %v", err)
	}
	defer conn.Close()
	cancel()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("Write after dial context ended: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("Read after dial context ended: %v", err)
	}
	if string(buf) != "ping" {
		t.Errorf("echo = %q, want %q", buf, "ping")
	}
}
