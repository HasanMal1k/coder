package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// modelServer answers one chat completion with the given handler.
func modelServer(t *testing.T, handler http.HandlerFunc) *ChatCompletions {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &ChatCompletions{BaseURL: server.URL + "/v1/", APIKey: "key", Model: "deepseek-chat", Client: server.Client()}
}

func stream(w http.ResponseWriter, lines ...string) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	for _, line := range lines {
		fmt.Fprint(w, line+"\n\n")
		w.(http.Flusher).Flush()
	}
}

func TestStreamedReplyIsAssembled(t *testing.T) {
	var request map[string]any

	llm := modelServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("request to %s with %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		json.NewDecoder(r.Body).Decode(&request)

		stream(w,
			`: keep-alive`,
			`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":"Two "},"finish_reason":null}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":"machines."},"finish_reason":null}]}`,
			// Two tool calls, their arguments arriving in pieces and
			// interleaved, as providers send them.
			`data: {"choices":[{"index":0,"delta":{"content":null,"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"create_machine","arguments":""}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"name\":"}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"create_machine","arguments":"{\"name\":\"web\"}"}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"api\"}"}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"total_tokens":42}}`,
			`data: [DONE]`,
		)
	})

	var pieces []string
	msg, err := llm.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, toolDefs, func(piece string) {
		pieces = append(pieces, piece)
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if msg.Role != "assistant" || msg.Content != "Two machines." {
		t.Errorf("message = %+v", msg)
	}
	if !equal(pieces, []string{"Two ", "machines."}) {
		t.Errorf("pieces = %q, want each piece of text as it arrived", pieces)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("tool calls = %+v, want 2", msg.ToolCalls)
	}
	first, second := msg.ToolCalls[0], msg.ToolCalls[1]
	if first.ID != "call_a" || first.Type != "function" || first.Function.Name != "create_machine" || first.Function.Arguments != `{"name":"api"}` {
		t.Errorf("first call = %+v", first)
	}
	if second.ID != "call_b" || second.Function.Arguments != `{"name":"web"}` {
		t.Errorf("second call = %+v", second)
	}

	if request["model"] != "deepseek-chat" || request["stream"] != true || len(request["tools"].([]any)) != len(toolDefs) {
		t.Errorf("request = model %v, stream %v, %d tools", request["model"], request["stream"], len(request["tools"].([]any)))
	}
}

func TestAReplyCutOffIsAnErrorNotAHalfMessage(t *testing.T) {
	llm := modelServer(t, func(w http.ResponseWriter, r *http.Request) {
		stream(w,
			`data: {"choices":[{"index":0,"delta":{"content":"I will delete"},"finish_reason":null}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"stop_machine","arguments":"{\"na"}}]},"finish_reason":null}]}`,
		)
		// The connection ends here, with no finish and no [DONE].
	})

	msg, err := llm.Complete(context.Background(), nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "cut off") {
		t.Errorf("Complete = %+v, %v; want an error, never half a tool call to act on", msg, err)
	}
}

func TestAStreamWithoutDoneStillFinishes(t *testing.T) {
	llm := modelServer(t, func(w http.ResponseWriter, r *http.Request) {
		stream(w,
			`data: {"choices":[{"index":0,"delta":{"content":"Done."},"finish_reason":null}]}`,
			`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		)
	})

	msg, err := llm.Complete(context.Background(), nil, nil, nil)
	if err != nil || msg.Content != "Done." {
		t.Errorf("Complete = %+v, %v", msg, err)
	}
}

func TestToolCallsWithoutIDsGetOne(t *testing.T) {
	llm := modelServer(t, func(w http.ResponseWriter, r *http.Request) {
		stream(w,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"list_machines","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
		)
	})

	msg, err := llm.Complete(context.Background(), nil, nil, nil)
	if err != nil || len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID == "" {
		t.Errorf("Complete = %+v, %v; results are matched to calls by ID, so there must be one", msg, err)
	}
}

func TestAnErrorInsideTheStreamIsReported(t *testing.T) {
	llm := modelServer(t, func(w http.ResponseWriter, r *http.Request) {
		stream(w,
			`data: {"choices":[{"index":0,"delta":{"content":"Let me"},"finish_reason":null}]}`,
			`data: {"error":{"message":"server overloaded"}}`,
		)
	})

	_, err := llm.Complete(context.Background(), nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "server overloaded") {
		t.Errorf("err = %v, want the model's own message", err)
	}
}

func TestAReplyThatArrivesWholeIsAccepted(t *testing.T) {
	llm := modelServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"list_machines","arguments":"{}"}}]}}]}`)
	})

	msg, err := llm.Complete(context.Background(), nil, nil, func(string) { t.Error("text reported for a reply with none") })
	if err != nil || msg.Content != "" || len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "c1" {
		t.Errorf("Complete = %+v, %v", msg, err)
	}
}

func TestTheModelsRefusalIsReported(t *testing.T) {
	llm := modelServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"Authentication Fails, Your api key is invalid"}}`)
	})

	_, err := llm.Complete(context.Background(), nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "api key is invalid") {
		t.Errorf("err = %v, want the status and the model's message", err)
	}
}

func TestTheToolsAreValidSchemas(t *testing.T) {
	names := map[string]bool{}
	for _, def := range toolDefs {
		names[def.Function.Name] = true

		var schema struct {
			Type       string                    `json:"type"`
			Properties map[string]map[string]any `json:"properties"`
			Required   []string                  `json:"required"`
		}
		if err := json.Unmarshal(def.Function.Parameters, &schema); err != nil || schema.Type != "object" {
			t.Errorf("%s: parameters are not an object schema: %v", def.Function.Name, err)
		}
		for _, required := range schema.Required {
			if _, ok := schema.Properties[required]; !ok {
				t.Errorf("%s requires %q, which it does not define", def.Function.Name, required)
			}
		}

		// A model's API may check each schema strictly and refuse the whole
		// request over one keyword of the wrong type, at any depth.
		var whole any
		if err := json.Unmarshal(def.Function.Parameters, &whole); err != nil {
			t.Fatalf("%s: %v", def.Function.Name, err)
		}
		for _, problem := range schemaProblems(def.Function.Name, whole) {
			t.Error(problem)
		}
	}
	for _, name := range []string{toolListMachines, toolCreateMachine, toolStartMachine, toolStopMachine, toolCodeOnMachine, toolListAgents, toolStopAgent, toolShowPreview, toolAskUser, toolProposeArchitecture, toolUpdateArchitecture} {
		if !names[name] {
			t.Errorf("no definition for %s", name)
		}
	}

	// The model is told the sizes that exist, from the one place they are
	// defined.
	for _, want := range []string{"small (1 vCPU, 1 GB memory)", "large (4 vCPU, 4 GB memory)", "10, 20 or 50"} {
		if !strings.Contains(systemPrompt, want) {
			t.Errorf("the instructions don't mention %q", want)
		}
	}
	if strings.Contains(systemPrompt, "%!") {
		t.Error("the instructions were formatted with the wrong number of values")
	}
}

// schemaProblems finds keywords in a JSON schema whose values are not of the
// type JSON Schema gives them, looking inside every nested schema.
func schemaProblems(path string, value any) []string {
	schema, ok := value.(map[string]any)
	if !ok {
		return []string{fmt.Sprintf("%s: a schema must be an object, got %T", path, value)}
	}

	var problems []string
	wrong := func(keyword, want string) {
		problems = append(problems, fmt.Sprintf("%s: %q must be %s, got %v", path, keyword, want, schema[keyword]))
	}

	for keyword, v := range schema {
		switch keyword {
		case "type", "description":
			if _, ok := v.(string); !ok {
				wrong(keyword, "a string")
			}
		case "required", "enum":
			list, ok := v.([]any)
			if !ok || len(list) == 0 {
				wrong(keyword, "a list with something in it")
			}
		case "properties":
			properties, ok := v.(map[string]any)
			if !ok {
				wrong(keyword, "an object")
			}
			for name, property := range properties {
				problems = append(problems, schemaProblems(path+"."+name, property)...)
			}
		case "items":
			problems = append(problems, schemaProblems(path+"[]", v)...)
		default:
			problems = append(problems, fmt.Sprintf("%s: unexpected keyword %q", path, keyword))
		}
	}
	return problems
}
