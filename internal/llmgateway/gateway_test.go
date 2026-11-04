package llmgateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HasanMal1k/coder/internal/instance"
)

const (
	machineID    = "0b0f3a4e-7c1d-4f7e-9a53-2f6f0c7d9e11"
	machineToken = "guest-token"
	machineKey   = machineID + "." + machineToken
)

type fakeGuests struct{}

func (fakeGuests) AuthenticateGuest(_ context.Context, id, token string) error {
	if id != machineID || token != machineToken {
		return instance.ErrGuestUnauthorized
	}
	return nil
}

type fakeTasks struct {
	mu     sync.Mutex
	active map[string]bool
}

func (f *fakeTasks) TaskActive(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active[id]
}

// upstream records what the model provider was sent.
type upstream struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  int
	auth   string
	body   map[string]any
}

func newGateway(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *upstream, *fakeTasks) {
	t.Helper()

	up := &upstream{}
	up.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.mu.Lock()
		up.calls++
		up.auth = r.Header.Get("Authorization")
		up.body = nil
		json.NewDecoder(r.Body).Decode(&up.body)
		up.mu.Unlock()

		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("upstream was asked for %s", r.URL.Path)
		}
		handler(w, r)
	}))
	t.Cleanup(up.server.Close)

	tasks := &fakeTasks{active: map[string]bool{machineID: true}}
	gateway := New(Config{BaseURL: up.server.URL + "/v1/", APIKey: "real-key", Model: "deepseek-chat"}, fakeGuests{}, tasks)

	front := httptest.NewServer(http.HandlerFunc(gateway.ChatCompletions))
	t.Cleanup(front.Close)

	return front, up, tasks
}

func call(t *testing.T, url, key, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call gateway: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func errorMessage(t *testing.T, resp *http.Response) string {
	t.Helper()
	var out struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("error response is not in the OpenAI shape: %v", err)
	}
	return out.Error.Message
}

func TestForwardsWithTheRealKeyAndModel(t *testing.T) {
	front, up, _ := newGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	})

	resp := call(t, front.URL, machineKey, `{"model":"anything-the-machine-likes","messages":[{"role":"user","content":"hello"}],"stream":false,"temperature":0.2}`)
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 || !strings.Contains(string(body), `"content":"hi"`) {
		t.Fatalf("response = %d %s", resp.StatusCode, body)
	}
	if up.auth != "Bearer real-key" {
		t.Errorf("upstream was sent %q, want the server's key and never the machine's", up.auth)
	}
	if up.body["model"] != "deepseek-chat" {
		t.Errorf("model = %v, want the configured one whatever the machine asked for", up.body["model"])
	}
	if up.body["temperature"] != 0.2 || up.body["messages"] == nil {
		t.Errorf("the rest of the request was not passed through: %v", up.body)
	}
}

func TestSetsHowHardTheModelThinks(t *testing.T) {
	front, up, _ := newGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"hi"}}]}`)
	})
	body := `{"messages":[{"role":"user","content":"hello"}],"reasoning_effort":"low"}`

	// Unset, the machine's own choice passes through like the rest.
	call(t, front.URL, machineKey, body)
	if up.body["reasoning_effort"] != "low" {
		t.Errorf("reasoning_effort = %v, want the request's own when none is configured", up.body["reasoning_effort"])
	}

	gateway := New(Config{BaseURL: up.server.URL + "/v1/", APIKey: "real-key", Model: "deepseek-chat", ReasoningEffort: "max"},
		fakeGuests{}, &fakeTasks{active: map[string]bool{machineID: true}})
	configured := httptest.NewServer(http.HandlerFunc(gateway.ChatCompletions))
	t.Cleanup(configured.Close)

	call(t, configured.URL, machineKey, body)
	if up.body["reasoning_effort"] != "max" {
		t.Errorf("reasoning_effort = %v, want the configured one whatever the machine asked for", up.body["reasoning_effort"])
	}
}

func TestRefusesWithoutValidMachineCredentials(t *testing.T) {
	front, up, _ := newGateway(t, func(http.ResponseWriter, *http.Request) {})

	for name, key := range map[string]string{
		"no key":         "",
		"wrong token":    machineID + ".nope",
		"not a machine":  "sk-something",
		"unknown id":     "11111111-2222-4333-8444-555555555555." + machineToken,
		"the server key": "real-key",
	} {
		resp := call(t, front.URL, key, `{}`)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, resp.StatusCode)
		}
	}
	if up.calls != 0 {
		t.Errorf("upstream was called %d times for unauthenticated requests", up.calls)
	}
}

func TestRefusesAMachineWithNoTaskInProgress(t *testing.T) {
	front, up, tasks := newGateway(t, func(http.ResponseWriter, *http.Request) {})
	tasks.mu.Lock()
	tasks.active = map[string]bool{}
	tasks.mu.Unlock()

	resp := call(t, front.URL, machineKey, `{"messages":[]}`)

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if msg := errorMessage(t, resp); !strings.Contains(msg, "only while the assistant is working") {
		t.Errorf("message = %q", msg)
	}
	if up.calls != 0 {
		t.Error("upstream was called for a machine with no task")
	}
}

func TestAcceptsTheIDInAnyCase(t *testing.T) {
	_, up, tasks := newGateway(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) })

	// Credentials are checked with the ID as it was sent; tasks are looked
	// up by its canonical, lowercase form.
	gateway := New(Config{BaseURL: up.server.URL + "/v1", APIKey: "real-key", Model: "m"}, fakeGuestsAnyCase{}, tasks)
	server := httptest.NewServer(http.HandlerFunc(gateway.ChatCompletions))
	defer server.Close()

	resp := call(t, server.URL, strings.ToUpper(machineID)+"."+machineToken, `{}`)
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

type fakeGuestsAnyCase struct{}

func (fakeGuestsAnyCase) AuthenticateGuest(_ context.Context, id, token string) error {
	if !strings.EqualFold(id, machineID) || token != machineToken {
		return instance.ErrGuestUnauthorized
	}
	return nil
}

func TestStreamsAsTheModelProduces(t *testing.T) {
	release := make(chan struct{})
	front, _, _ := newGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"n\":1}\n\n")
		w.(http.Flusher).Flush()
		<-release
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer close(release)

	resp := call(t, front.URL, machineKey, `{"stream":true}`)
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q", got)
	}

	// The first event must arrive while the model is still "thinking".
	first := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(resp.Body).ReadString('\n')
		first <- line
	}()

	select {
	case line := <-first:
		if line != "data: {\"n\":1}\n" {
			t.Errorf("first line = %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first event was held back until the response ended")
	}
}

func TestPassesTheModelsErrorsOn(t *testing.T) {
	front, _, _ := newGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		fmt.Fprint(w, `{"error":{"message":"Insufficient Balance"}}`)
	})

	resp := call(t, front.URL, machineKey, `{}`)
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Errorf("status = %d, want the model's own 402", resp.StatusCode)
	}
	if msg := errorMessage(t, resp); msg != "Insufficient Balance" {
		t.Errorf("message = %q", msg)
	}
}

func TestRejectsBadRequests(t *testing.T) {
	front, up, _ := newGateway(t, func(http.ResponseWriter, *http.Request) {})

	if resp := call(t, front.URL, machineKey, `not json`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("not JSON: status = %d, want 400", resp.StatusCode)
	}

	huge := `{"messages":"` + strings.Repeat("x", maxRequestBody) + `"}`
	if resp := call(t, front.URL, machineKey, huge); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized: status = %d, want 413", resp.StatusCode)
	}
	if up.calls != 0 {
		t.Error("upstream was called for a bad request")
	}
}

func TestDisabledWithoutAKey(t *testing.T) {
	gateway := New(Config{BaseURL: "http://unused", Model: "m"}, fakeGuests{}, &fakeTasks{})
	server := httptest.NewServer(http.HandlerFunc(gateway.ChatCompletions))
	defer server.Close()

	if resp := call(t, server.URL, machineKey, `{}`); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}
