// Package portproxy serves programs running inside machines (the editor, a
// user's dev server) to the browser. Each port of each machine gets its own
// hostname, <port>-<instance id>.<workspace host>, and therefore its own
// browser origin: code running on a user's machine can't read the app's
// storage, or another machine's.
//
// A browser gets in with a ticket: the app asks the API for a URL (which
// requires the user's access token and ownership of the instance), and
// loading that URL trades the single-use ticket in it for a session cookie
// on the machine's origin.
package portproxy

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/HasanMal1k/coder/internal/instance"
)

const (
	// authPath is where a ticket is exchanged for a session cookie. It is
	// reserved on every machine origin.
	authPath = "/__coder/auth"

	cookieName = "coder_workspace_session"

	// dialSuffix marks the fake hostnames the proxy's HTTP transport is asked
	// to connect to; they encode which machine port to dial.
	dialSuffix = ".guest"
)

// Dialer opens connections to ports on machines.
type Dialer interface {
	DialPort(ctx context.Context, instanceID string, port int) (net.Conn, error)
}

type Config struct {
	// Host is the host (with port, if not the default) that machine origins
	// are subdomains of, e.g. "localhost:8080".
	Host string
	// Scheme is "http" or "https": how browsers reach Host.
	Scheme string
	// Secret is the server's signing secret. Grants are signed with a key
	// derived from it.
	Secret []byte
}

type Proxy struct {
	host   string
	scheme string
	// secureCookies is whether browsers will accept a Secure cookie on
	// machine origins, which the cross-site cookie attributes require.
	secureCookies bool

	grants  *grants
	forward *httputil.ReverseProxy
}

type targetKey struct{}

func New(cfg Config, dialer Dialer) *Proxy {
	host := strings.ToLower(cfg.Host)
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	p := &Proxy{
		host:   host,
		scheme: cfg.Scheme,
		// Browsers treat localhost and its subdomains as secure even over
		// plain HTTP.
		secureCookies: cfg.Scheme == "https" || hostname == "localhost" || strings.HasSuffix(hostname, ".localhost"),
		grants:        newGrants(cfg.Secret),
	}

	p.forward = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			t := pr.In.Context().Value(targetKey{}).(target)

			// Not a real address: the transport's DialContext below turns it
			// back into a machine and a port. Using a distinct name per
			// target also makes the transport pool connections per target.
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = strconv.Itoa(t.port) + "." + t.instanceID + dialSuffix

			// The program should see the hostname the browser used, so the
			// URLs and origin checks it produces are right.
			pr.Out.Host = pr.In.Host
			pr.SetXForwarded()
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
				t, err := parseDialAddr(addr)
				if err != nil {
					return nil, err
				}
				return dialer.DialPort(ctx, t.instanceID, t.port)
			},
			MaxIdleConnsPerHost: 16,
			IdleConnTimeout:     90 * time.Second,
			// Pass responses through exactly as the program sent them.
			DisableCompression: true,
		},
		// Flush every write straight away: dev servers and the editor stream
		// events that must not sit in a buffer.
		FlushInterval: -1,
		ErrorHandler:  p.forwardError,
	}

	return p
}

// URL returns a link that lets userID's browser open a port on an instance,
// landing on path. The caller must already have checked that the user owns
// the instance. The link works once and only for about a minute.
func (p *Proxy) URL(userID, instanceID string, port int, path string) (string, error) {
	ticket, err := p.grants.issueTicket(userID, target{instanceID: instanceID, port: port})
	if err != nil {
		return "", fmt.Errorf("issue ticket: %w", err)
	}

	query := url.Values{}
	query.Set("ticket", ticket)
	query.Set("next", safeNext(path))

	link := url.URL{
		Scheme:   p.scheme,
		Host:     strconv.Itoa(port) + "-" + instanceID + "." + p.host,
		Path:     authPath,
		RawQuery: query.Encode(),
	}

	return link.String(), nil
}

// Wrap returns a handler that serves requests for machine origins itself and
// passes everything else to next.
func (p *Proxy) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		label, ok := strings.CutSuffix(strings.ToLower(r.Host), "."+p.host)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		t, ok := parseLabel(label)
		if !ok {
			p.errorPage(w, r, http.StatusNotFound, "Not found", "This address doesn't belong to a machine.", false)
			return
		}

		p.serve(w, r, t)
	})
}

func (p *Proxy) serve(w http.ResponseWriter, r *http.Request, t target) {
	if r.URL.Path == authPath {
		p.authenticate(w, r, t)
		return
	}

	cookie, err := r.Cookie(cookieName)
	if err != nil || p.grants.verifySession(cookie.Value, t) != nil {
		p.errorPage(w, r, http.StatusUnauthorized, "Sign-in needed",
			"Your access to this machine has expired. Open it again from the app.", false)
		return
	}

	if !p.originAllowed(r, t) {
		p.errorPage(w, r, http.StatusForbidden, "Forbidden",
			"This request came from another site.", false)
		return
	}

	// The session is ours; the program on the machine has no business
	// seeing it.
	stripCookie(r, cookieName)

	ctx := context.WithValue(r.Context(), targetKey{}, t)
	p.forward.ServeHTTP(w, r.WithContext(ctx))
}

// authenticate trades the ticket in the URL for a session cookie on this
// machine origin, then redirects to where the user was headed.
func (p *Proxy) authenticate(w http.ResponseWriter, r *http.Request, t target) {
	// The URL contains a credential: keep it out of caches and Referer
	// headers.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")

	userID, err := p.grants.redeemTicket(r.URL.Query().Get("ticket"), t)
	if err != nil {
		p.errorPage(w, r, http.StatusUnauthorized, "Link expired",
			"This link has already been used or has expired. Open the machine again from the app.", false)
		return
	}

	session, err := p.grants.issueSession(userID, t)
	if err != nil {
		log.Printf("portproxy: issue session: %v", err)
		p.errorPage(w, r, http.StatusInternalServerError, "Something went wrong", "Please try again.", false)
		return
	}

	cookie := &http.Cookie{
		Name:     cookieName,
		Value:    session,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	if p.secureCookies {
		// The machine is shown in a frame inside the app. Deployed as
		// intended, app and machines are one site and any cookie would do.
		// These attributes keep the frame working when they are not (the app
		// opened as plain localhost, say): a cross-site frame only gets
		// cookies marked SameSite=None and Secure, and Partitioned ties the
		// cookie to the embedding site so no other site can frame the
		// machine and ride the session. Some features that need a service
		// worker (the editor's webviews) still require the same-site setup.
		cookie.Secure = true
		cookie.SameSite = http.SameSiteNoneMode
		cookie.Partitioned = true
	}
	http.SetCookie(w, cookie)

	http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusFound)
}

// originAllowed rejects requests that another site's page caused the browser
// to make. Ordinary navigations carry no Origin header; scripted requests and
// WebSockets do, and are allowed only from this machine's own origins.
func (p *Proxy) originAllowed(r *http.Request, t target) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	label, ok := strings.CutSuffix(strings.ToLower(u.Host), "."+p.host)
	if !ok {
		return false
	}
	from, ok := parseLabel(label)

	// Any port of the same machine may call any other: a frontend on one
	// port talking to an API on another is a normal setup.
	return ok && from.instanceID == t.instanceID
}

func (p *Proxy) forwardError(w http.ResponseWriter, r *http.Request, err error) {
	t, _ := r.Context().Value(targetKey{}).(target)

	switch {
	case errors.Is(err, context.Canceled):
		// The browser went away; there is nobody to answer.
	case errors.Is(err, instance.ErrGuestOffline):
		p.errorPage(w, r, http.StatusServiceUnavailable, "Machine not connected",
			"The machine is stopped or still starting up.", true)
	case errors.Is(err, instance.ErrPortClosed):
		p.errorPage(w, r, http.StatusBadGateway, fmt.Sprintf("Nothing on port %d yet", t.port),
			"No program on the machine is listening on this port.", true)
	case errors.Is(err, instance.ErrPortUnsupported):
		p.errorPage(w, r, http.StatusBadGateway, "Machine needs recreating",
			"This machine was created before this feature existed. Create a new machine to use it.", false)
	default:
		log.Printf("portproxy: %s %s: %v", r.Method, r.Host, err)
		p.errorPage(w, r, http.StatusBadGateway, "Can't reach the machine",
			"The connection to the machine failed.", true)
	}
}

var errorTemplate = template.Must(template.New("error").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Retry}}<meta http-equiv="refresh" content="2">{{end}}
<title>{{.Title}}</title>
<style>
  html { color-scheme: dark; }
  body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #0b0d10; color: #e7e9ec;
         font: 15px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; }
  main { max-width: 26rem; padding: 2rem; text-align: center; }
  h1 { margin: 0 0 .5rem; font-size: 1.125rem; }
  p { margin: 0; color: #8f97a3; }
  .retry { margin-top: 1rem; font-size: .8125rem; }
</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
{{if .Retry}}<p class="retry">Retrying automatically…</p>{{end}}
</main>
</body>
</html>
`))

// errorPage reports a problem. A browser loading a page gets HTML, which
// reloads itself when retry is set (the editor takes a moment to come up
// after a machine starts); anything else gets plain text.
func (p *Proxy) errorPage(w http.ResponseWriter, r *http.Request, status int, title, message string, retry bool) {
	w.Header().Set("Cache-Control", "no-store")

	wantsHTML := r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html")
	if !wantsHTML {
		http.Error(w, title+": "+message, status)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	errorTemplate.Execute(w, struct {
		Title, Message string
		Retry          bool
	}{title, message, retry})
}

// parseLabel reads the "<port>-<instance id>" part of a machine hostname.
func parseLabel(label string) (target, bool) {
	portText, id, ok := strings.Cut(label, "-")
	if !ok {
		return target{}, false
	}

	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != portText {
		return target{}, false
	}

	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return target{}, false
	}

	return target{instanceID: id, port: port}, true
}

// parseDialAddr reverses the fake address built in the proxy's Rewrite.
func parseDialAddr(addr string) (target, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return target{}, err
	}

	name, ok := strings.CutSuffix(host, dialSuffix)
	portText, id, cut := strings.Cut(name, ".")
	port, err := strconv.Atoi(portText)
	if !ok || !cut || err != nil {
		return target{}, fmt.Errorf("portproxy: unexpected dial address %q", addr)
	}

	return target{instanceID: id, port: port}, nil
}

// safeNext returns path if it is a path on this origin, and "/" otherwise, so
// the redirect after signing in can't be pointed at another site.
func safeNext(path string) string {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "\\") {
		return "/"
	}

	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" {
		return "/"
	}

	return path
}

// stripCookie removes one cookie from the request, leaving the rest of the
// Cookie header exactly as the browser sent it.
func stripCookie(r *http.Request, name string) {
	var kept []string
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			part = strings.TrimSpace(part)
			if part == "" || strings.HasPrefix(part, name+"=") {
				continue
			}
			kept = append(kept, part)
		}
	}

	if len(kept) == 0 {
		r.Header.Del("Cookie")
		return
	}
	r.Header.Set("Cookie", strings.Join(kept, "; "))
}
