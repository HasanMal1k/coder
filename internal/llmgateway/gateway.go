// Package llmgateway lets the coding agents inside machines use the
// platform's model without ever holding its API key. An agent calls this
// gateway as if it were the model provider; the gateway checks which machine
// is asking, and forwards the request upstream with the real key.
package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/HasanMal1k/coder/internal/instance"
)

// maxRequestBody caps one request to the model. Conversations with a coding
// agent grow large, but not without limit.
const maxRequestBody = 16 << 20

// Guests checks a machine's credentials. *instance.Service implements it.
type Guests interface {
	AuthenticateGuest(ctx context.Context, id, token string) error
}

// Tasks says whether the assistant has a machine working on something.
// *assistant.Fleet implements it.
type Tasks interface {
	TaskActive(instanceID string) bool
}

type Config struct {
	// BaseURL is the root of the upstream OpenAI-compatible API, e.g.
	// "https://api.deepseek.com/v1".
	BaseURL string
	APIKey  string
	// Model replaces whatever model a request names: machines don't choose.
	Model string
}

type Gateway struct {
	cfg    Config
	guests Guests
	tasks  Tasks
	client *http.Client
}

func New(cfg Config, guests Guests, tasks Tasks) *Gateway {
	return &Gateway{
		cfg:    cfg,
		guests: guests,
		tasks:  tasks,
		client: &http.Client{
			// No overall timeout: responses are streams that last as long as
			// the model takes. The wait for one to begin is bounded.
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				ResponseHeaderTimeout: 5 * time.Minute,
				IdleConnTimeout:       90 * time.Second,
				ForceAttemptHTTP2:     true,
			},
		},
	}
}

// ChatCompletions handles POST <guestapi.ModelPath>/chat/completions.
//
// The caller authenticates as a machine, with "<instance id>.<guest token>"
// as its API key. It is served only while the assistant has given that
// machine a task: the key sits on a machine its user has root on, and must
// not be worth anything the rest of the time.
func (g *Gateway) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if g.cfg.APIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "no model is configured on this server")
		return
	}

	key, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	rawID, token, _ := strings.Cut(key, ".")

	if err := g.guests.AuthenticateGuest(r.Context(), rawID, token); err != nil {
		if errors.Is(err, instance.ErrGuestUnauthorized) {
			writeError(w, http.StatusUnauthorized, "invalid machine credentials")
			return
		}
		log.Printf("llmgateway: authenticate machine: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// The same form of the ID the assistant tracks tasks by.
	instanceID := uuid.MustParse(rawID).String()
	if !g.tasks.TaskActive(instanceID) {
		writeError(w, http.StatusForbidden, "the model is available to this machine only while the assistant is working on it")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "could not read the request")
		return
	}

	// Everything in the request passes through untouched except the model.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not a JSON object")
		return
	}
	fields["model"], _ = json.Marshal(g.cfg.Model)
	body, _ = json.Marshal(fields)

	upstream, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		strings.TrimRight(g.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		log.Printf("llmgateway: build upstream request: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)
	if accept := r.Header.Get("Accept"); accept != "" {
		upstream.Header.Set("Accept", accept)
	}

	resp, err := g.client.Do(upstream)
	if err != nil {
		if r.Context().Err() != nil {
			// The machine went away; there is nobody to answer.
			return
		}
		log.Printf("llmgateway: call model for %s: %v", instanceID, err)
		writeError(w, http.StatusBadGateway, "could not reach the model")
		return
	}
	defer resp.Body.Close()

	// The model's own errors (rate limits, an exhausted balance) are passed
	// on as they are: the agent reports them, and they end up in the chat.
	for _, name := range []string{"Content-Type", "Cache-Control", "Retry-After"} {
		if value := resp.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Flush as it arrives: the response is a stream of small events, and the
	// agent acts on each one.
	flusher := http.NewResponseController(w)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
			flusher.Flush()
		}
		if readErr != nil {
			return
		}
	}
}

// writeError answers in the shape OpenAI-compatible clients expect, so the
// message reaches whoever reads the agent's error.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "coder_gateway_error",
		},
	})
}
