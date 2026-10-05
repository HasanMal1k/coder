package portproxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"

	"github.com/HasanMal1k/coder/internal/instance"
)

const (
	instanceA = "5357ebb3-d8d9-4b33-b0bf-d730a78740d5"
	instanceB = "034ed82b-5b7f-4d56-b53d-5e1bb15534cc"
	testHost  = "ws.test"
)

// fakeDialer stands in for the tunnel: every "machine port" is the same
// local test server, unless err is set.
type fakeDialer struct {
	addr string
	err  error

	mu sync.Mutex
	// inUse counts MachineConnected calls not yet released, per machine.
	inUse map[string]int
	// used counts every MachineConnected call, per machine.
	used map[string]int
}

func (d *fakeDialer) MachineConnected(instanceID string) func() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.inUse == nil {
		d.inUse, d.used = map[string]int{}, map[string]int{}
	}
	d.inUse[instanceID]++
	d.used[instanceID]++

	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.inUse[instanceID]--
	}
}

func (d *fakeDialer) counts(instanceID string) (inUse, used int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inUse[instanceID], d.used[instanceID]
}

func (d *fakeDialer) DialPort(ctx context.Context, instanceID string, port int) (net.Conn, error) {
	if d.err != nil {
		return nil, d.err
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", d.addr)
}

// seen records what the program on the machine received.
type seen struct {
	mu     sync.Mutex
	host   string
	cookie string
	uri    string
}

func (s *seen) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.host, s.cookie, s.uri = r.Host, r.Header.Get("Cookie"), r.URL.RequestURI()
}

func (s *seen) get() (host, cookie, uri string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.host, s.cookie, s.uri
}

type harness struct {
	proxy   *Proxy
	dialer  *fakeDialer
	machine *seen
	front   *httptest.Server
}

// newHarness starts a program "on the machine", and the proxy in front of an
// API that answers "api".
func newHarness(t *testing.T) *harness {
	t.Helper()

	machine := &seen{}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		machine.record(r)

		if r.URL.Path == "/ws" {
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			typ, data, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			conn.Write(r.Context(), typ, append([]byte("echo: "), data...))
			conn.Close(websocket.StatusNormalClosure, "")
			return
		}

		// The program sets a cookie of its own, as real apps do.
		http.SetCookie(w, &http.Cookie{Name: "app", Value: "1", Path: "/"})
		io.WriteString(w, "hello from the machine")
	}))
	t.Cleanup(backend.Close)

	dialer := &fakeDialer{addr: backend.Listener.Addr().String()}
	proxy := New(Config{Host: testHost, Scheme: "http", Secret: []byte("test secret")}, dialer)

	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "api")
	})
	front := httptest.NewServer(proxy.Wrap(api))
	t.Cleanup(front.Close)

	return &harness{proxy: proxy, dialer: dialer, machine: machine, front: front}
}

// browser returns a client that, like a browser with working DNS, reaches the
// proxy whatever hostname it asks for, and keeps cookies per host.
func (h *harness) browser(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, h.front.Listener.Addr().String())
			},
		},
	}
	t.Cleanup(client.CloseIdleConnections)

	return client
}

func machineURL(instanceID string, port, path string) string {
	return "http://" + port + "-" + instanceID + "." + testHost + path
}

func get(t *testing.T, client *http.Client, url string, headers ...string) (int, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, string(body)
}

// signIn opens instanceA's port 8000 the way the app does.
func (h *harness) signIn(t *testing.T, client *http.Client) {
	t.Helper()

	link, err := h.proxy.URL("user-1", instanceA, 8000, "/")
	if err != nil {
		t.Fatal(err)
	}
	if status, body := get(t, client, link); status != http.StatusOK {
		t.Fatalf("sign in: status %d, body %q", status, body)
	}
}

func TestTicketGrantsAccessToThePort(t *testing.T) {
	h := newHarness(t)
	client := h.browser(t)

	if status, _ := get(t, client, machineURL(instanceA, "8000", "/")); status != http.StatusUnauthorized {
		t.Fatalf("without a session: status %d, want 401", status)
	}

	link, err := h.proxy.URL("user-1", instanceA, 8000, "/hello?x=1")
	if err != nil {
		t.Fatal(err)
	}
	status, body := get(t, client, link)
	if status != http.StatusOK || body != "hello from the machine" {
		t.Fatalf("after following the link: status %d, body %q", status, body)
	}

	host, cookie, uri := h.machine.get()
	if uri != "/hello?x=1" {
		t.Errorf("machine saw %q, want the path the link was issued for", uri)
	}
	if want := "8000-" + instanceA + "." + testHost; host != want {
		t.Errorf("machine saw Host %q, want %q", host, want)
	}
	if strings.Contains(cookie, cookieName) {
		t.Errorf("our session cookie reached the machine: %q", cookie)
	}

	// A second request uses the cookie alone, and carries the program's own
	// cookie through untouched.
	if status, _ := get(t, client, machineURL(instanceA, "8000", "/again")); status != http.StatusOK {
		t.Fatalf("second request: status %d, want 200", status)
	}
	_, cookie, _ = h.machine.get()
	if cookie != "app=1" {
		t.Errorf("machine saw Cookie %q, want only its own %q", cookie, "app=1")
	}
}

func TestTicketIsSingleUse(t *testing.T) {
	h := newHarness(t)

	link, err := h.proxy.URL("user-1", instanceA, 8000, "/")
	if err != nil {
		t.Fatal(err)
	}

	if status, _ := get(t, h.browser(t), link); status != http.StatusOK {
		t.Fatalf("first use: status %d, want 200", status)
	}
	// Someone who found the link later, in a different browser.
	if status, _ := get(t, h.browser(t), link); status != http.StatusUnauthorized {
		t.Fatalf("second use: status %d, want 401", status)
	}
}

func TestGrantsAreBoundToInstanceAndPort(t *testing.T) {
	h := newHarness(t)

	link, err := h.proxy.URL("user-1", instanceA, 8000, "/")
	if err != nil {
		t.Fatal(err)
	}

	for name, other := range map[string]string{
		"another instance": strings.Replace(link, instanceA, instanceB, 1),
		"another port":     strings.Replace(link, "8000-", "9000-", 1),
	} {
		if status, _ := get(t, h.browser(t), other); status != http.StatusUnauthorized {
			t.Errorf("ticket used for %s: status %d, want 401", name, status)
		}
	}

	// A session cookie for one port, presented to another machine or port.
	session, err := h.proxy.grants.issueSession("user-1", target{instanceID: instanceA, port: 8000})
	if err != nil {
		t.Fatal(err)
	}
	for name, url := range map[string]string{
		"another instance": machineURL(instanceB, "8000", "/"),
		"another port":     machineURL(instanceA, "9000", "/"),
	} {
		status, _ := get(t, h.browser(t), url, "Cookie", cookieName+"="+session)
		if status != http.StatusUnauthorized {
			t.Errorf("session used for %s: status %d, want 401", name, status)
		}
	}
	if status, _ := get(t, h.browser(t), machineURL(instanceA, "8000", "/"), "Cookie", cookieName+"="+session); status != http.StatusOK {
		t.Errorf("session used where it was issued: status %d, want 200", status)
	}
}

func TestGrantsRejectTokensSignedWithTheRawSecret(t *testing.T) {
	// API access tokens are signed with the raw secret. Someone holding one
	// must not be able to dress it up as a session.
	h := newHarness(t)

	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, grantClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Audience:  jwt.ClaimStrings{audienceSession},
			ID:        "x",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Instance: instanceA,
		Port:     8000,
	}).SignedString([]byte("test secret"))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.proxy.grants.verifySession(forged, target{instanceID: instanceA, port: 8000}); err == nil {
		t.Fatal("a token signed with the raw secret was accepted as a session")
	}
}

func TestGrantsExpireAndAreNotInterchangeable(t *testing.T) {
	g := newGrants([]byte("test secret"))
	tgt := target{instanceID: instanceA, port: 8000}

	expired, err := g.sign("user-1", tgt, audienceSession, -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if g.verifySession(expired, tgt) == nil {
		t.Error("an expired session was accepted")
	}

	ticket, _ := g.issueTicket("user-1", tgt)
	if g.verifySession(ticket, tgt) == nil {
		t.Error("a ticket was accepted as a session")
	}

	session, _ := g.issueSession("user-1", tgt)
	if _, err := g.redeemTicket(session, tgt); err == nil {
		t.Error("a session was accepted as a ticket")
	}
}

func TestCrossSiteRequestsAreRejected(t *testing.T) {
	h := newHarness(t)
	client := h.browser(t)
	h.signIn(t, client)

	url := machineURL(instanceA, "8000", "/")
	tests := []struct {
		origin string
		want   int
	}{
		{"http://8000-" + instanceA + "." + testHost, http.StatusOK},        // itself
		{"http://3000-" + instanceA + "." + testHost, http.StatusOK},        // another port, same machine
		{"http://8000-" + instanceB + "." + testHost, http.StatusForbidden}, // another machine
		{"http://" + testHost, http.StatusForbidden},
		{"http://evil.example", http.StatusForbidden},
		{"null", http.StatusForbidden},
	}

	for _, tt := range tests {
		if status, _ := get(t, client, url, "Origin", tt.origin); status != tt.want {
			t.Errorf("Origin %q: status %d, want %d", tt.origin, status, tt.want)
		}
	}
}

func TestOtherHostsReachTheAPI(t *testing.T) {
	h := newHarness(t)
	client := h.browser(t)

	for _, url := range []string{"http://" + testHost + "/instances", "http://api.example/instances"} {
		if status, body := get(t, client, url); status != http.StatusOK || body != "api" {
			t.Errorf("%s: status %d, body %q; want the API", url, status, body)
		}
	}

	// Under the workspace host but not a machine: must not fall through to
	// the API.
	for _, host := range []string{"junk", "8000-not-a-uuid", "0-" + instanceA, "99999-" + instanceA} {
		if status, body := get(t, client, "http://"+host+"."+testHost+"/"); status != http.StatusNotFound || body == "api" {
			t.Errorf("host %q: status %d, body %q; want 404", host, status, body)
		}
	}
}

func TestMachineProblemsAreExplained(t *testing.T) {
	tests := []struct {
		err       error
		want      int
		wantRetry bool
	}{
		{instance.ErrGuestOffline, http.StatusServiceUnavailable, true},
		{instance.ErrPortClosed, http.StatusBadGateway, true},
		{instance.ErrPortUnsupported, http.StatusBadGateway, false},
	}

	for _, tt := range tests {
		h := newHarness(t)
		client := h.browser(t)
		h.signIn(t, client)
		h.dialer.err = tt.err
		// Connections made while signing in would bypass the failing dial.
		h.proxy.forward.Transport.(*http.Transport).CloseIdleConnections()

		status, body := get(t, client, machineURL(instanceA, "8000", "/"), "Accept", "text/html")
		if status != tt.want {
			t.Errorf("%v: status %d, want %d", tt.err, status, tt.want)
		}
		if retries := strings.Contains(body, `http-equiv="refresh"`); retries != tt.wantRetry {
			t.Errorf("%v: page retries = %v, want %v", tt.err, retries, tt.wantRetry)
		}
	}
}

func TestRequestsCountAsUsingTheMachine(t *testing.T) {
	h := newHarness(t)
	client := h.browser(t)

	// A request without a session never reaches the machine, so it must not
	// keep the machine alive either.
	get(t, client, machineURL(instanceA, "8000", "/"))
	if _, used := h.dialer.counts(instanceA); used != 0 {
		t.Fatalf("an unauthenticated request was counted as use (%d)", used)
	}

	h.signIn(t, client)
	if _, used := h.dialer.counts(instanceA); used == 0 {
		t.Error("a proxied request was not counted as use")
	}
	// The client has its response a moment before the server's handler
	// returns and gives the use back, so this is waited for, not asserted
	// at once.
	h.waitUnused(t, "after the requests finished")

	// A WebSocket counts for as long as it is open.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws://8000-"+instanceA+"."+testHost+"/ws", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if inUse, _ := h.dialer.counts(instanceA); inUse != 1 {
		t.Errorf("in use = %d while a WebSocket is open, want 1", inUse)
	}

	// The test backend answers one message and closes.
	conn.Write(ctx, websocket.MessageText, []byte("bye"))
	conn.Read(ctx)
	conn.CloseNow()

	h.waitUnused(t, "after the WebSocket closed")
}

// waitUnused waits for nothing to be using instance A.
func (h *harness) waitUnused(t *testing.T, when string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if inUse, _ := h.dialer.counts(instanceA); inUse == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("machine still counted as in use %s", when)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// routes is a Router with one route.
type routes struct {
	from   target
	prefix string
	to     target
}

func (r routes) Route(_ context.Context, instanceID string, port int, path string) (string, int, bool) {
	if instanceID == r.from.instanceID && port == r.from.port && strings.HasPrefix(path, r.prefix) {
		return r.to.instanceID, r.to.port, true
	}
	return "", 0, false
}

func TestAPathCanLeadToAnotherMachine(t *testing.T) {
	h := newHarness(t)
	// /api on machine A's port 8000 belongs to machine B's port 9000.
	h.proxy.router = routes{
		from:   target{instanceID: instanceA, port: 8000},
		prefix: "/api",
		to:     target{instanceID: instanceB, port: 9000},
	}
	client := h.browser(t)
	h.signIn(t, client)

	// Which machine served a request is told by which one was counted as
	// in use for it.
	usedA := func() int { _, used := h.dialer.counts(instanceA); return used }
	usedB := func() int { _, used := h.dialer.counts(instanceB); return used }

	// The browser is signed in to A only, and asks A.
	beforeA, beforeB := usedA(), usedB()
	status, _ := get(t, client, machineURL(instanceA, "8000", "/api/orders"))
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if usedB() != beforeB+1 || usedA() != beforeA {
		t.Errorf("a routed request used A %d times and B %d times, want B once and A not at all", usedA()-beforeA, usedB()-beforeB)
	}

	// Anything else on A stays on A.
	beforeA, beforeB = usedA(), usedB()
	get(t, client, machineURL(instanceA, "8000", "/index.html"))
	if usedA() != beforeA+1 || usedB() != beforeB {
		t.Errorf("a path with no route used A %d times and B %d times, want A once", usedA()-beforeA, usedB()-beforeB)
	}

	// The route gives no access of its own: B's address still needs B's
	// sign-in.
	if status, _ := get(t, client, machineURL(instanceB, "9000", "/api/orders")); status != http.StatusUnauthorized {
		t.Errorf("B's own address without a session for it: %d, want 401", status)
	}
}

func TestWebSocketsPassThrough(t *testing.T) {
	h := newHarness(t)
	client := h.browser(t)
	h.signIn(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws://8000-"+instanceA+"."+testHost+"/ws", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	if err := conn.Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "echo: ping" {
		t.Errorf("got %q, want %q", data, "echo: ping")
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	signInResponse := func(host, scheme string) string {
		proxy := New(Config{Host: host, Scheme: scheme, Secret: []byte("s")}, &fakeDialer{})
		link, err := proxy.URL("user-1", instanceA, 8000, "/")
		if err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, link, nil)
		rec := httptest.NewRecorder()
		proxy.Wrap(http.NotFoundHandler()).ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("%s://%s: status %d, want a redirect", scheme, host, rec.Code)
		}
		return rec.Header().Get("Set-Cookie")
	}

	// Shown in a frame inside the app, so the cookie must be usable
	// cross-site wherever the browser allows a Secure cookie.
	for _, origin := range [][2]string{{"localhost:8080", "http"}, {"coder.localhost:8080", "http"}, {"preview.example.dev", "https"}} {
		cookie := signInResponse(origin[0], origin[1])
		for _, attr := range []string{"HttpOnly", "Secure", "SameSite=None", "Partitioned", "Path=/"} {
			if !strings.Contains(cookie, attr) {
				t.Errorf("%s://%s: cookie %q lacks %s", origin[1], origin[0], cookie, attr)
			}
		}
		if strings.Contains(cookie, "Domain=") {
			t.Errorf("%s://%s: cookie %q must be host-only", origin[1], origin[0], cookie)
		}
	}

	// Plain HTTP on a real hostname can't have a Secure cookie at all.
	cookie := signInResponse("ws.example.test", "http")
	if strings.Contains(cookie, "Secure") || !strings.Contains(cookie, "SameSite=Lax") || !strings.Contains(cookie, "HttpOnly") {
		t.Errorf("plain http: cookie %q, want HttpOnly, SameSite=Lax and no Secure", cookie)
	}
}

func TestParseLabel(t *testing.T) {
	valid := map[string]target{
		"8000-" + instanceA:  {instanceID: instanceA, port: 8000},
		"1-" + instanceA:     {instanceID: instanceA, port: 1},
		"65535-" + instanceA: {instanceID: instanceA, port: 65535},
	}
	for label, want := range valid {
		if got, ok := parseLabel(label); !ok || got != want {
			t.Errorf("parseLabel(%q) = %v, %v; want %v", label, got, ok, want)
		}
	}

	invalid := []string{
		"", instanceA, "8000", "8000-", "-" + instanceA,
		"0-" + instanceA, "65536-" + instanceA, "08000-" + instanceA, "+80-" + instanceA,
		"8000-" + strings.ToUpper(instanceA),
		"8000-" + strings.ReplaceAll(instanceA, "-", ""),
		"8000-" + instanceA + "x",
		"80.00-" + instanceA,
	}
	for _, label := range invalid {
		if got, ok := parseLabel(label); ok {
			t.Errorf("parseLabel(%q) = %v, true; want it rejected", label, got)
		}
	}
}

func TestSafeNext(t *testing.T) {
	tests := map[string]string{
		"/":                         "/",
		"/?folder=/home/coder":      "/?folder=/home/coder",
		"/a/b?c=d#e":                "/a/b?c=d#e",
		"":                          "/",
		"relative":                  "/",
		"//evil.example":            "/",
		"/\\evil.example":           "/",
		"https://evil.example/":     "/",
		"javascript:alert(1)":       "/",
		"/ok/../../still-on-origin": "/ok/../../still-on-origin",
	}
	for in, want := range tests {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
