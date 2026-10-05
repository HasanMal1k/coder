package instance

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dialTo returns a dialer that connects to a test server, standing in for
// the tunnel into a machine.
func dialTo(server *httptest.Server) func(context.Context) (net.Conn, error) {
	return func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", server.Listener.Addr().String())
	}
}

func TestRequestReachesThePortAndBringsBackTheAnswer(t *testing.T) {
	var got *http.Request
	var sent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		body, _ := io.ReadAll(r.Body)
		sent = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Add("X-Thing", "one")
		w.Header().Add("X-Thing", "two")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"id":7}`)
	}))
	defer server.Close()

	req := PortRequest{
		Method: "post",
		Path:   "/orders?dry=1",
		Headers: map[string]string{
			"Content-Type": "application/json",
			"X-Token":      "abc",
			// Not the caller's to set: these describe the connection.
			"Host":       "evil.example",
			"Connection": "keep-alive",
		},
		Body: []byte(`{"item":"tea"}`),
	}
	if err := req.validate(); err != nil {
		t.Fatal(err)
	}

	resp, err := roundTrip(context.Background(), 8000, req, dialTo(server))
	if err != nil {
		t.Fatal(err)
	}

	if got.Method != http.MethodPost || got.URL.Path != "/orders" || got.URL.RawQuery != "dry=1" {
		t.Errorf("the port was asked %s %s?%s", got.Method, got.URL.Path, got.URL.RawQuery)
	}
	if got.Host != "localhost:8000" {
		t.Errorf("Host = %q, want the machine's own name for the port", got.Host)
	}
	if got.Header.Get("X-Token") != "abc" || got.Header.Get("Content-Type") != "application/json" {
		t.Errorf("headers were not passed on: %v", got.Header)
	}
	if sent != `{"item":"tea"}` {
		t.Errorf("body = %q", sent)
	}

	if resp.Status != http.StatusCreated || string(resp.Body) != `{"id":7}` || resp.Truncated {
		t.Errorf("response = %d %q truncated=%v", resp.Status, resp.Body, resp.Truncated)
	}
	if resp.Headers["Content-Type"] != "application/json" || resp.Headers["X-Thing"] != "one, two" {
		t.Errorf("response headers = %v", resp.Headers)
	}
}

func TestRequestReturnsErrorStatusesAndRedirectsAsTheyAre(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			http.Redirect(w, r, "/new", http.StatusFound)
		default:
			http.Error(w, "no such thing", http.StatusNotFound)
		}
	}))
	defer server.Close()

	moved, err := roundTrip(context.Background(), 80, PortRequest{Method: "GET", Path: "/old"}, dialTo(server))
	if err != nil {
		t.Fatal(err)
	}
	if moved.Status != http.StatusFound || moved.Headers["Location"] != "/new" {
		t.Errorf("a redirect was followed or lost: %d %v", moved.Status, moved.Headers)
	}

	missing, err := roundTrip(context.Background(), 80, PortRequest{Method: "GET", Path: "/nothing"}, dialTo(server))
	if err != nil {
		t.Fatalf("a 404 from the machine is an answer, not a failure: %v", err)
	}
	if missing.Status != http.StatusNotFound || !strings.Contains(string(missing.Body), "no such thing") {
		t.Errorf("response = %d %q", missing.Status, missing.Body)
	}
}

func TestRequestUndoesCompression(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Errorf("Accept-Encoding = %q, want the client's own", r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("Content-Encoding", "gzip")
		zipped := gzip.NewWriter(w)
		io.WriteString(zipped, "plain words")
		zipped.Close()
	}))
	defer server.Close()

	// A caller's own Accept-Encoding would leave the body compressed.
	req := PortRequest{Method: "GET", Path: "/", Headers: map[string]string{"Accept-Encoding": "br"}}
	resp, err := roundTrip(context.Background(), 80, req, dialTo(server))
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "plain words" {
		t.Errorf("body = %q", resp.Body)
	}
}

func TestRequestKeepsOnlyTheStartOfAHugeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x", maxPortResponse+500))
	}))
	defer server.Close()

	resp, err := roundTrip(context.Background(), 80, PortRequest{Method: "GET", Path: "/"}, dialTo(server))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Truncated || len(resp.Body) != maxPortResponse {
		t.Errorf("truncated=%v len=%d, want the first %d bytes and a note that there was more", resp.Truncated, len(resp.Body), maxPortResponse)
	}
}

func TestRequestSaysWhyThereWasNoAnswer(t *testing.T) {
	closed := func(context.Context) (net.Conn, error) { return nil, ErrPortClosed }
	if _, err := roundTrip(context.Background(), 80, PortRequest{Method: "GET", Path: "/"}, closed); !errors.Is(err, ErrPortClosed) {
		t.Errorf("nothing listening: err = %v, want ErrPortClosed", err)
	}

	offline := func(context.Context) (net.Conn, error) { return nil, ErrGuestOffline }
	if _, err := roundTrip(context.Background(), 80, PortRequest{Method: "GET", Path: "/"}, offline); !errors.Is(err, ErrGuestOffline) {
		t.Errorf("machine unreachable: err = %v, want ErrGuestOffline", err)
	}

	// Something is listening, but it doesn't speak HTTP.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			io.WriteString(conn, "SSH-2.0-OpenSSH_9.6\r\n")
			conn.Close()
		}
	}()
	notHTTP := func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", listener.Addr().String())
	}
	if _, err := roundTrip(context.Background(), 22, PortRequest{Method: "GET", Path: "/"}, notHTTP); !errors.Is(err, ErrBadResponse) {
		t.Errorf("not HTTP: err = %v, want ErrBadResponse", err)
	}

	// The caller gave up.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slow := func(ctx context.Context) (net.Conn, error) { <-ctx.Done(); return nil, ctx.Err() }
	if _, err := roundTrip(ctx, 80, PortRequest{Method: "GET", Path: "/"}, slow); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: err = %v, want context.Canceled", err)
	}
}

func TestOnlyRequestsToThePortItselfAreAccepted(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		ok           bool
	}{
		{"GET", "/", true},
		{"", "", true}, // a plain GET of the root
		{"delete", "/orders/7", true},
		{"GET", "/search?q=a%20b", true},
		{"TRACE", "/", false},
		{"CONNECT", "/", false},
		{"GET", "orders", false},
		{"GET", "http://example.com/", false},
		{"GET", "//example.com/", false},
		{"GET", "/a b", true}, // sent as /a%20b
		{"GET", "/a\r\nX-Injected: 1", false},
	} {
		req := PortRequest{Method: tc.method, Path: tc.path}
		err := req.validate()
		if tc.ok && err != nil {
			t.Errorf("%s %q: %v, want it accepted", tc.method, tc.path, err)
		}
		if !tc.ok && !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s %q: err = %v, want ErrInvalidRequest", tc.method, tc.path, err)
		}
	}

	req := PortRequest{}
	req.validate()
	if req.Method != "GET" || req.Path != "/" {
		t.Errorf("an empty request became %s %q, want GET /", req.Method, req.Path)
	}
}
