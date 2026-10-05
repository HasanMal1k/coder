package instance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalidRequest means a request for a machine's port was not one
	// that can be made: an unknown method, or a path that is not a path.
	ErrInvalidRequest = errors.New("invalid request")

	// ErrRequestTimeout means whatever is listening took too long to answer.
	ErrRequestTimeout = errors.New("the request took too long")

	// ErrBadResponse means something is listening on the port, but what it
	// sent back was not an HTTP response.
	ErrBadResponse = errors.New("the port did not answer with HTTP")
)

const (
	// portRequestTimeout bounds one request to a machine's port, start to
	// finish.
	portRequestTimeout = 30 * time.Second

	// maxPortResponse is how much of a response's body is kept. It is read
	// into memory and sent on as JSON, so it is bounded.
	maxPortResponse = 2 << 20
)

// portMethods are the methods a request to a machine's port may use.
var portMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodOptions: true,
}

// unforwarded are the headers of a request that are not passed on: those
// that describe the connection and not the request, and those the HTTP
// client sets itself from what it is sending.
var unforwarded = map[string]bool{
	"Host": true, "Content-Length": true, "Connection": true, "Keep-Alive": true,
	"Proxy-Connection": true, "Transfer-Encoding": true, "Upgrade": true, "Te": true, "Trailer": true,
	// Left to the client, which then also undoes the compression it asked for.
	"Accept-Encoding": true,
}

// PortRequest is an HTTP request to make to something listening inside a
// machine.
type PortRequest struct {
	Method string
	// Path is the path to ask for, with any query: "/users?page=2".
	Path    string
	Headers map[string]string
	Body    []byte
}

// PortResponse is what came back.
type PortResponse struct {
	Status  int
	Headers map[string]string
	Body    []byte
	// Truncated says Body is only the start of what was sent.
	Truncated bool
	Duration  time.Duration
}

// Request makes an HTTP request to a port inside one of ownerID's instances
// and returns the response, whatever its status: a 404 from the machine is a
// successful call. Redirects are returned, not followed. It is how the
// frontend reads an API's description and tries its endpoints without the
// browser having to be allowed onto that API's origin.
func (s *Service) Request(ctx context.Context, ownerID, id string, port int, req PortRequest) (*PortResponse, error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("%w: port must be between 1 and 65535", ErrInvalidRequest)
	}
	if err := req.validate(); err != nil {
		return nil, err
	}

	// This is the ownership check.
	inst, err := s.Get(ctx, ownerID, id)
	if err != nil {
		return nil, err
	}
	if inst.Status != StatusRunning {
		return nil, ErrNotRunning
	}

	return roundTrip(ctx, port, req, func(ctx context.Context) (net.Conn, error) {
		return s.DialPort(ctx, inst.ID, port)
	})
}

func (r *PortRequest) validate() error {
	r.Method = strings.ToUpper(strings.TrimSpace(r.Method))
	if r.Method == "" {
		r.Method = http.MethodGet
	}
	if !portMethods[r.Method] {
		return fmt.Errorf("%w: method %q is not supported", ErrInvalidRequest, r.Method)
	}

	if r.Path == "" {
		r.Path = "/"
	}
	// Only a path: a full URL here would name some other host to call.
	if !strings.HasPrefix(r.Path, "/") || strings.HasPrefix(r.Path, "//") {
		return fmt.Errorf("%w: path must start with a single /", ErrInvalidRequest)
	}
	if _, err := url.ParseRequestURI(r.Path); err != nil {
		return fmt.Errorf("%w: path is not valid", ErrInvalidRequest)
	}

	return nil
}

// roundTrip sends req over a connection from dial and reads the response.
func roundTrip(ctx context.Context, port int, req PortRequest, dial func(context.Context) (net.Conn, error)) (*PortResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, portRequestTimeout)
	defer cancel()

	// What the machine sees is a request to itself.
	target := "http://localhost:" + strconv.Itoa(port) + req.Path
	out, err := http.NewRequestWithContext(ctx, req.Method, target, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	for name, value := range req.Headers {
		if !unforwarded[http.CanonicalHeaderKey(name)] {
			out.Header.Set(name, value)
		}
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dial(ctx)
		},
		// One connection per request: nothing here outlives the call.
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	started := time.Now()
	resp, err := client.Do(out)
	if err != nil {
		return nil, explainRoundTrip(ctx, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPortResponse+1))
	if err != nil {
		return nil, explainRoundTrip(ctx, err)
	}
	truncated := len(body) > maxPortResponse
	if truncated {
		body = body[:maxPortResponse]
	}

	headers := make(map[string]string, len(resp.Header))
	for name, values := range resp.Header {
		headers[name] = strings.Join(values, ", ")
	}

	return &PortResponse{
		Status:    resp.StatusCode,
		Headers:   headers,
		Body:      body,
		Truncated: truncated,
		Duration:  time.Since(started),
	}, nil
}

// explainRoundTrip turns a failure to get a response into one of this
// package's errors, so callers can say what actually went wrong.
func explainRoundTrip(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, ErrPortClosed), errors.Is(err, ErrGuestOffline), errors.Is(err, ErrPortUnsupported):
		return err
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return ErrRequestTimeout
	case ctx.Err() != nil:
		return ctx.Err()
	}
	return fmt.Errorf("%w: %v", ErrBadResponse, err)
}
