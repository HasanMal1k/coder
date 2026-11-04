package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// maxToolCalls bounds how many tools one reply may ask for.
const maxToolCalls = 64

// Message is one entry in the conversation sent to the model, in the shape
// the OpenAI-compatible chat completions API uses (DeepSeek speaks it).
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ReasoningContent is what a thinking model reasoned before it replied.
	// DeepSeek requires it back, on every later request of a conversation
	// with tools, or the model loses the thread between tool calls.
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

// ToolCall is the model asking for a tool to be run.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name string `json:"name"`
	// Arguments is a JSON object, as text: that is how the API delivers it.
	Arguments string `json:"arguments"`
}

// ToolDef describes a tool the model may call.
type ToolDef struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// LLM produces the assistant's next message for a conversation.
type LLM interface {
	// Complete returns the model's reply. If onText is not nil it is called
	// with each piece of the reply's text as the model writes it; the pieces
	// together are the Content of the message returned.
	Complete(ctx context.Context, messages []Message, tools []ToolDef, onText func(string)) (Message, error)
}

// ChatCompletions calls an OpenAI-compatible chat completions endpoint.
type ChatCompletions struct {
	// BaseURL is the API root, e.g. "https://api.deepseek.com/v1".
	BaseURL string
	APIKey  string
	Model   string
	Client  *http.Client
}

const (
	// maxErrorBody caps how much of an upstream error response is kept.
	maxErrorBody = 2000
	// maxStreamLine caps one line of a streamed reply.
	maxStreamLine = 1 << 20
)

// Complete asks for the reply as a stream, so its text can be shown while it
// is being written, and assembles the pieces into one message.
func (c *ChatCompletions) Complete(ctx context.Context, messages []Message, tools []ToolDef, onText func(string)) (Message, error) {
	body, err := json.Marshal(map[string]any{
		"model":    c.Model,
		"messages": messages,
		"tools":    tools,
		"stream":   true,
	})
	if err != nil {
		return Message{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.Client.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("call model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return Message{}, fmt.Errorf("model returned %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}

	// A server that ignores "stream" answers with the whole reply at once.
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return readWhole(resp.Body, onText)
	}

	return readStream(resp.Body, onText)
}

func (c *ChatCompletions) endpoint() string {
	return strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
}

// readWhole decodes a reply that arrived in one piece.
func readWhole(body io.Reader, onText func(string)) (Message, error) {
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return Message{}, fmt.Errorf("decode model response: %w", err)
	}
	if len(out.Choices) == 0 {
		return Message{}, fmt.Errorf("model returned no choices")
	}

	msg := out.Choices[0].Message
	msg.Role = "assistant"
	if onText != nil && msg.Content != "" {
		onText(msg.Content)
	}

	return msg, nil
}

// readStream assembles a reply that arrives as server-sent events.
//
// Text comes as fragments to be joined. A tool call comes in fragments too:
// the first carries its ID and name, the rest carry its arguments a few
// characters at a time, and "index" says which call a fragment belongs to.
func readStream(body io.Reader, onText func(string)) (Message, error) {
	var (
		text      strings.Builder
		reasoning strings.Builder
		calls     []*ToolCall
		finished  bool
	)

	reader := bufio.NewReaderSize(body, 64*1024)
	for {
		line, err := readLine(reader, maxStreamLine)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return Message{}, fmt.Errorf("read model response: %w", err)
		}

		// Blank lines separate events, and lines starting with ":" are
		// comments some providers send to keep the connection open.
		data, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok {
			continue
		}
		data = bytes.TrimSpace(data)
		if string(data) == "[DONE]" {
			finished = true
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int          `json:"index"`
						ID       string       `json:"id"`
						Function FunctionCall `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &chunk); err != nil {
			return Message{}, fmt.Errorf("decode model response: %w", err)
		}
		if chunk.Error != nil {
			return Message{}, fmt.Errorf("model failed part-way through its reply: %s", chunk.Error.Message)
		}

		for _, choice := range chunk.Choices {
			// Kept, not shown: the person sees the reply, not the thinking.
			reasoning.WriteString(choice.Delta.ReasoningContent)

			if piece := choice.Delta.Content; piece != "" {
				text.WriteString(piece)
				if onText != nil {
					onText(piece)
				}
			}

			for _, fragment := range choice.Delta.ToolCalls {
				if fragment.Index < 0 || fragment.Index > maxToolCalls {
					return Message{}, fmt.Errorf("decode model response: tool call index %d", fragment.Index)
				}
				for len(calls) <= fragment.Index {
					calls = append(calls, &ToolCall{Type: "function"})
				}

				call := calls[fragment.Index]
				if fragment.ID != "" {
					call.ID = fragment.ID
				}
				call.Function.Name += fragment.Function.Name
				call.Function.Arguments += fragment.Function.Arguments
			}

			if choice.FinishReason != nil {
				finished = true
			}
		}
	}

	// A connection that drops mid-reply ends the stream like a finished one
	// does. Half a message, or half a tool call, must not be acted on.
	if !finished {
		return Message{}, errors.New("the model's reply was cut off")
	}

	msg := Message{Role: "assistant", Content: text.String(), ReasoningContent: reasoning.String()}
	for _, call := range calls {
		if call.ID == "" {
			// Results are matched to requests by ID, so every call needs one.
			call.ID = "call_" + uuid.NewString()
		}
		msg.ToolCalls = append(msg.ToolCalls, *call)
	}

	return msg, nil
}
