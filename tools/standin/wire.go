package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// This file is the model's API as far as the platform uses it: the
// OpenAI-compatible chat completions format, which DeepSeek speaks. A request
// is read, and a reply is written either whole or as a stream.

// request is one call to POST /chat/completions.
type request struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Tools    []toolDef `json:"tools"`
	Stream   bool      `json:"stream"`
	// StreamOptions asks for the token count as the stream's last event.
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

// message is one entry in the conversation.
type message struct {
	Role string `json:"role"`
	// Content is text, or a list of parts some of which are text. Both are
	// sent: the assistant sends text, the coding agent sends parts.
	Content    content    `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// content is a message's text, however it was sent.
type content string

func (c *content) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		*c = content(text)
		return nil
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		// null, or a shape this program has no use for.
		*c = ""
		return nil
	}

	var texts []string
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	*c = content(strings.Join(texts, "\n"))

	return nil
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name string `json:"name"`
	// Arguments is a JSON object, as text.
	Arguments string `json:"arguments"`
}

type toolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name       string          `json:"name"`
		Parameters json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// reply is what the stand-in says next: some text, some tool calls, or both.
type reply struct {
	Text  string
	Calls []call
	// Pause is how long to wait before answering, where the time a model
	// takes to think is part of what is being checked.
	Pause time.Duration
}

// call is one tool the reply asks for.
type call struct {
	// Step names the step of the script this call belongs to. It is put in
	// the call's ID, which comes back in the conversation, and that is how
	// the script knows how far it has got.
	Step string
	Name string
	Args map[string]any
}

// say is a reply of text alone.
func say(text string) reply {
	return reply{Text: text}
}

// ask is a reply that asks for one tool.
func ask(step, name string, args map[string]any) reply {
	return reply{Calls: []call{{Step: step, Name: name, Args: args}}}
}

// with adds a line of text in front of a reply's tool calls.
func (r reply) with(text string) reply {
	r.Text = text
	return r
}

// after makes a reply take a while to come, as a model's does.
func (r reply) after(pause time.Duration) reply {
	r.Pause = pause
	return r
}

// and adds another tool call to a reply: tools asked for together are run
// together.
func (r reply) and(step, name string, args map[string]any) reply {
	r.Calls = append(r.Calls, call{Step: step, Name: name, Args: args})
	return r
}

// callPrefix starts every tool call ID the stand-in makes, followed by the
// step and something unique.
const callPrefix = "call_"

// callID makes an ID for a tool call belonging to step. The step can be read
// back out of it with stepOf.
func callID(step string) string {
	var unique [4]byte
	rand.Read(unique[:])

	return callPrefix + step + "_" + hex.EncodeToString(unique[:])
}

// stepOf returns the step a tool call ID was made for, or "" if the ID is not
// one of the stand-in's.
func stepOf(id string) string {
	rest, ok := strings.CutPrefix(id, callPrefix)
	if !ok {
		return ""
	}
	end := strings.LastIndex(rest, "_")
	if end < 0 {
		return ""
	}

	return rest[:end]
}

// toolCalls turns a reply's calls into the API's shape.
func (r reply) toolCalls() []toolCall {
	calls := make([]toolCall, 0, len(r.Calls))
	for _, c := range r.Calls {
		args := c.Args
		if args == nil {
			args = map[string]any{}
		}
		raw, _ := json.Marshal(args)

		calls = append(calls, toolCall{
			ID:       callID(c.Step),
			Type:     "function",
			Function: functionCall{Name: c.Name, Arguments: string(raw)},
		})
	}

	return calls
}

func (r reply) finishReason() string {
	if len(r.Calls) > 0 {
		return "tool_calls"
	}
	return "stop"
}

// usage is a made-up token count. Clients that ask for one expect the fields.
func (r reply) usage(req *request) map[string]int {
	prompt := 0
	for _, m := range req.Messages {
		prompt += len(m.Content) / 4
	}
	completion := len(r.Text)/4 + 16*len(r.Calls)

	return map[string]int{
		"prompt_tokens":     prompt,
		"completion_tokens": completion,
		"total_tokens":      prompt + completion,
	}
}

// writeWhole answers with the reply in one piece.
func writeWhole(w http.ResponseWriter, req *request, r reply) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":      "standin-" + hex.EncodeToString(randomBytes(6)),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":       "assistant",
				"content":    r.Text,
				"tool_calls": r.toolCalls(),
			},
			"finish_reason": r.finishReason(),
		}},
		"usage": r.usage(req),
	})
}

// fragment is how many bytes of a tool call's arguments go in one event. A
// real model sends them a few characters at a time; a client that only works
// when they arrive whole is a client with a bug.
const fragment = 48

// writeStream answers with the reply as server-sent events: the text a few
// words at a time, then each tool call, its arguments in fragments. pace is
// how long to wait between events.
func writeStream(w http.ResponseWriter, req *request, r reply, pace time.Duration) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher := http.NewResponseController(w)
	id := "standin-" + hex.EncodeToString(randomBytes(6))
	created := time.Now().Unix()

	event := func(delta map[string]any, finish any) {
		raw, _ := json.Marshal(map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   req.Model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		fmt.Fprintf(w, "data: %s\n\n", raw)
		flusher.Flush()
		time.Sleep(pace)
	}

	event(map[string]any{"role": "assistant", "content": ""}, nil)

	for _, piece := range pieces(r.Text) {
		event(map[string]any{"content": piece}, nil)
	}

	for i, tc := range r.toolCalls() {
		// The first event of a call names it; the rest carry its arguments.
		event(map[string]any{"tool_calls": []map[string]any{{
			"index":    i,
			"id":       tc.ID,
			"type":     "function",
			"function": map[string]any{"name": tc.Function.Name, "arguments": ""},
		}}}, nil)

		for args := tc.Function.Arguments; args != ""; {
			n := min(fragment, len(args))
			// Never split a character in two.
			for n < len(args) && !isRuneStart(args[n]) {
				n++
			}
			event(map[string]any{"tool_calls": []map[string]any{{
				"index":    i,
				"function": map[string]any{"arguments": args[:n]},
			}}}, nil)
			args = args[n:]
		}
	}

	event(map[string]any{}, r.finishReason())

	if req.StreamOptions.IncludeUsage {
		raw, _ := json.Marshal(map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   req.Model,
			"choices": []any{},
			"usage":   r.usage(req),
		})
		fmt.Fprintf(w, "data: %s\n\n", raw)
	}

	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// pieces splits text into a few words at a time, keeping every character:
// joined again they are the text.
func pieces(text string) []string {
	const words = 3

	var out []string
	count, start := 0, 0
	for i, r := range text {
		if r == ' ' || r == '\n' {
			count++
			if count == words {
				out = append(out, text[start:i+1])
				start, count = i+1, 0
			}
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}

	return out
}

func isRuneStart(b byte) bool {
	return b&0xC0 != 0x80
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

// writeAPIError answers in the shape the model's API reports its own errors.
func writeAPIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}
