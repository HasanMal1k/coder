// standin answers in place of the model, so that the whole platform can be
// run and checked without spending anyone's credits. It speaks the
// OpenAI-compatible chat completions API, which is what the assistant and the
// coding agents call, and answers from a script (see shop.go).
//
// It is not part of the product: it is a Go module of its own, nothing in
// the server imports it, and the server needs no change to use it. Start the
// server pointed at it:
//
//	go run . &
//	DEEPSEEK_API_KEY=standin LLM_BASE_URL=http://localhost:9099/v1 go run ./cmd/server
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// maxBody caps one request. The platform's own gateway allows as much.
const maxBody = 16 << 20

type server struct {
	// pace is the wait between the events of a streamed reply.
	pace time.Duration
	// dump, if set, is a folder each request and reply is written to.
	dump string
	// seen counts requests, to name the dumped files in order.
	seen atomic.Int64
}

func main() {
	addr := flag.String("addr", "127.0.0.1:9099", "address to listen on")
	pace := flag.Duration("pace", 15*time.Millisecond, "wait between the events of a streamed reply")
	dump := flag.String("dump", "", "folder to write every request and reply to, for looking at what a client sends")
	flag.Parse()

	log.SetPrefix("standin: ")
	log.SetFlags(log.Ltime)

	s := &server{pace: *pace, dump: *dump}
	if s.dump != "" {
		if err := os.MkdirAll(s.dump, 0o755); err != nil {
			log.Fatal(err)
		}
	}

	mux := http.NewServeMux()
	// With and without /v1, since a base URL may or may not end in it.
	for _, prefix := range []string{"", "/v1"} {
		mux.HandleFunc("POST "+prefix+"/chat/completions", s.chatCompletions)
		mux.HandleFunc("GET "+prefix+"/models", models)
	}

	log.Printf("answering for the model on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

// models lists the one model there is, in the provider's shape.
func models(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   []map[string]any{{"id": "standin", "object": "model", "owned_by": "standin"}},
	})
}

func (s *server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "could not read the request")
		return
	}

	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "the request is not valid JSON: "+err.Error())
		return
	}

	// The real API turns down a request with a tool whose schema is wrong.
	// A stand-in that accepted anything once let such a schema through.
	if err := checkRequest(&req); err != nil {
		log.Printf("refused: %v", err)
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	c := conversationOf(&req)

	var (
		who string
		out reply
	)
	switch c.caller() {
	case callerAssistant:
		who, out = "assistant", assistantReply(c)
	case callerAgent:
		who = "an agent with no task"
		if t, ok := c.currentTask(); ok {
			who = fmt.Sprintf("agent %s on %s", t.Agent, t.Machine)
		}
		out = agentReply(c)
	default:
		// The coding agent asks for a title for each session.
		who, out = "something without tools", say("Shop build")
	}

	log.Printf("%s: %s", who, describe(out))
	s.record(raw, out)

	if out.Pause > 0 {
		select {
		case <-time.After(out.Pause):
		case <-r.Context().Done():
			return
		}
	}

	if req.Stream {
		writeStream(w, &req, out, s.pace)
	} else {
		writeWhole(w, &req, out)
	}
}

// checkRequest refuses what the real API would refuse.
func checkRequest(req *request) error {
	if len(req.Messages) == 0 {
		return errors.New("messages is empty")
	}

	for _, t := range req.Tools {
		name := t.Function.Name
		if name == "" {
			return errors.New("a tool has no name")
		}
		if len(t.Function.Parameters) == 0 {
			continue
		}

		var schema struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
			Required   json.RawMessage            `json:"required"`
		}
		if err := json.Unmarshal(t.Function.Parameters, &schema); err != nil {
			return fmt.Errorf("tool %q: parameters is not a JSON schema object", name)
		}
		if schema.Type != "object" {
			return fmt.Errorf("tool %q: parameters must have type \"object\"", name)
		}
		if schema.Required != nil {
			var required []string
			if err := json.Unmarshal(schema.Required, &required); err != nil || required == nil {
				return fmt.Errorf("tool %q: \"required\" must be a list of names, or left out", name)
			}
			for _, field := range required {
				if _, ok := schema.Properties[field]; !ok {
					return fmt.Errorf("tool %q: %q is required but is not one of its properties", name, field)
				}
			}
		}
	}

	return nil
}

// describe says in a line what a reply does, for the log.
func describe(r reply) string {
	if len(r.Calls) == 0 {
		return "says " + quote(r.Text)
	}

	var calls []string
	for _, c := range r.Calls {
		calls = append(calls, c.Name+" ("+c.Step+")")
	}

	return "asks for " + strings.Join(calls, ", ")
}

func quote(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		s = s[:77] + "..."
	}

	return `"` + s + `"`
}

// record writes a request and the reply to it into the dump folder.
func (s *server) record(raw []byte, out reply) {
	if s.dump == "" {
		return
	}

	n := s.seen.Add(1)
	answer, _ := json.MarshalIndent(map[string]any{"text": out.Text, "calls": out.Calls}, "", "  ")

	os.WriteFile(filepath.Join(s.dump, fmt.Sprintf("%04d-request.json", n)), raw, 0o644)
	os.WriteFile(filepath.Join(s.dump, fmt.Sprintf("%04d-reply.json", n)), answer, 0o644)
}
