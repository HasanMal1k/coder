package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// offered builds the tools of a request from their names.
func offered(names ...string) []toolDef {
	tools := make([]toolDef, len(names))
	for i, name := range names {
		tools[i].Type = "function"
		tools[i].Function.Name = name
		tools[i].Function.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
	}

	return tools
}

var (
	assistantTools = offered("list_machines", "create_machine", "code_on_machine", "show_preview", "ask_user", "propose_architecture")
	agentTools     = offered("bash", "write", "read", "team_run_service", "team_share_note", "team_ask_agent", "team_ask_owner")
)

// play runs a script as its client would: it asks for the next reply, adds
// it to the conversation, and answers each tool call with results(call).
// It stops when the reply asks for no tools, or after limit turns.
func play(t *testing.T, script func(conversation) reply, tools []toolDef, messages []message, results func(toolCall) string, limit int) ([]message, []reply) {
	t.Helper()

	var replies []reply
	for range limit {
		out := script(conversationOf(&request{Messages: messages, Tools: tools}))
		replies = append(replies, out)

		calls := out.toolCalls()
		messages = append(messages, message{Role: "assistant", Content: content(out.Text), ToolCalls: calls})
		if len(calls) == 0 {
			return messages, replies
		}
		for _, tc := range calls {
			messages = append(messages, message{Role: "tool", ToolCallID: tc.ID, Content: content(results(tc))})
		}
	}

	t.Fatalf("the script was still asking for tools after %d turns", limit)
	return nil, nil
}

func names(r reply) []string {
	var out []string
	for _, c := range r.Calls {
		out = append(out, c.Name)
	}

	return out
}

func TestAssistantDesignsThenBuilds(t *testing.T) {
	user := []message{{Role: "system", Content: "You are the assistant"}, {Role: "user", Content: "Build me a shop"}}

	// A question first, and nothing else.
	c := conversationOf(&request{Messages: user, Tools: assistantTools})
	first := assistantReply(c)
	if got := names(first); len(got) != 1 || got[0] != "ask_user" {
		t.Fatalf("first reply asks for %v, want ask_user", got)
	}

	// Asked for a change, it proposes again, differently; approved, it builds.
	proposals := 0
	history, replies := play(t, assistantReply, assistantTools, user, func(tc toolCall) string {
		switch tc.Function.Name {
		case "ask_user":
			return "A few dozen"
		case "propose_architecture":
			proposals++
			if proposals == 1 {
				return "The user has not approved the design. They said:\n\nMake the API machine medium"
			}
			if !strings.Contains(tc.Function.Arguments, `"size":"medium"`) {
				t.Errorf("the second proposal does not make the API's machine medium: %s", tc.Function.Arguments)
			}
			return approvedPrefix + ". This is what to build: ..."
		case "code_on_machine":
			if strings.Contains(tc.Function.Arguments, `"background":true`) {
				return `Agent "ci" is now working on "ci" in the background.`
			}
			return "Done."
		default:
			return "ok"
		}
	}, 20)

	if proposals != 2 {
		t.Fatalf("proposed %d times, want 2", proposals)
	}

	var asked []string
	for _, r := range replies {
		asked = append(asked, strings.Join(names(r), "+"))
	}
	want := []string{
		"ask_user",
		"propose_architecture",
		"propose_architecture",
		"create_machine+create_machine+create_machine+create_machine",
		"code_on_machine+code_on_machine",
		"code_on_machine",
		"code_on_machine",
		"", // the checks are in the background: it says so and stops
	}
	if strings.Join(asked, " | ") != strings.Join(want, " | ") {
		t.Fatalf("turns were\n  %v\nwant\n  %v", asked, want)
	}

	// The background agent finishing sets it going again: previews, then a
	// summary.
	history = append(history, message{Role: "user", Content: `[From the platform, not the user] Background agent "ci" on machine "ci" has finished.`})
	_, replies = play(t, assistantReply, assistantTools, history, func(toolCall) string { return "ok" }, 5)

	if got := names(replies[0]); len(got) != 2 || got[0] != "show_preview" {
		t.Fatalf("after the checks finished it asks for %v, want two show_preview", got)
	}
	if last := replies[len(replies)-1]; last.Text != shopSummary {
		t.Fatalf("it ends with %q, want the summary", last.Text)
	}
}

func TestAssistantStopsAtAFailure(t *testing.T) {
	history := []message{{Role: "user", Content: "Build me a shop"}}
	history, _ = play(t, assistantReply, assistantTools, history, func(tc toolCall) string {
		switch tc.Function.Name {
		case "propose_architecture":
			return approvedPrefix + "."
		case "create_machine":
			return "Error: the image for that stack has not been built"
		}
		return "ok"
	}, 20)

	last := history[len(history)-1]
	if !strings.Contains(string(last.Content), "did not work") || !strings.Contains(string(last.Content), "has not been built") {
		t.Fatalf("after a machine failed to be created it said %q", last.Content)
	}
}

func taskFor(agent, machine, text string) message {
	return message{Role: "user", Content: content(`You are tom, the "` + agent + `" agent: a coding agent on the machine "` + machine + `", Ubuntu Linux.` + "\n\nThe task:\n\n" + text)}
}

func TestAPIAgentAddsTheDescriptionWhenTold(t *testing.T) {
	served := 0
	paused := false
	_, replies := play(t, agentReply, agentTools, []message{taskFor("api", "api", "Build the API")}, func(tc toolCall) string {
		if tc.Function.Name == "team_run_service" && strings.Contains(tc.Function.Arguments, `"name":"api"`) {
			served++
			if served == 1 {
				return `The platform now keeps "api" running. Its output is in ~/.coder/logs/api.log. It is up and answering on port 8000. But this is a backend, and GET /openapi.json answered 404. Every backend must serve one.`
			}
			return `The platform now keeps "api" running. Its output is in ~/.coder/logs/api.log. It is up and answering on port 8000. Its API description is served at /openapi.json (4 paths).`
		}
		return "ok"
	}, 20)

	if served != 2 {
		t.Fatalf("registered the API %d times, want 2: once without its description and once with", served)
	}

	// The second version of the file has the description; the first does not.
	var written []string
	for _, r := range replies {
		for _, c := range r.Calls {
			if c.Name == "write" {
				written = append(written, c.Args["content"].(string))
				// Only the second takes a while to come.
				paused = r.Pause > 0
			}
		}
	}
	if !paused {
		t.Error("the description was added at once; a model takes a while, and the check of the Backend tab depends on it")
	}
	if len(written) != 2 || strings.Contains(written[0], "openapi") || !strings.Contains(written[1], `"/openapi.json"`) {
		t.Fatalf("wrote the server %d times; the first must lack the description and the second have it", len(written))
	}

	report := replies[len(replies)-1].Text
	if !strings.Contains(report, "Its API description is served at /openapi.json (4 paths).") {
		t.Fatalf("the report does not say the description is served: %q", report)
	}
}

func TestAPIAgentDoesNotRepeatItselfWhenNothingIsWrong(t *testing.T) {
	served := 0
	play(t, agentReply, agentTools, []message{taskFor("api", "api", "Build the API")}, func(tc toolCall) string {
		if tc.Function.Name == "team_run_service" && strings.Contains(tc.Function.Arguments, `"name":"api"`) {
			served++
			return `The platform now keeps "api" running. Its output is in ~/.coder/logs/api.log. It is up and answering on port 8000.`
		}
		return "ok"
	}, 20)

	if served != 1 {
		t.Fatalf("registered the API %d times, want 1", served)
	}
}

func TestAgentUsesItsNewestTask(t *testing.T) {
	// An agent keeps one conversation: an earlier, finished task is in it.
	history, _ := play(t, agentReply, agentTools, []message{taskFor("ci", "ci", "Check the API")}, func(toolCall) string { return "api: PASS" }, 10)

	history = append(history, taskFor("ci", "ci", "Check it again"))
	out := agentReply(conversationOf(&request{Messages: history, Tools: agentTools}))
	if got := names(out); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("given a second task it asks for %v, want to start again with bash", got)
	}
}

func TestAgentAnswersAQuestion(t *testing.T) {
	question := taskFor("api", "api", `Another agent, "storefront" on machine "web", is asking you something. Answer from what you built.`+"\n\nThe question:\n\nWhat does GET /api/products return?")
	out := agentReply(conversationOf(&request{Messages: []message{taskFor("api", "api", "Build the API"), {Role: "assistant", Content: "Built."}, question}, Tools: agentTools}))

	if len(out.Calls) != 0 || !strings.Contains(out.Text, "price_cents") {
		t.Fatalf("asked a question it replied %+v, want an answer in words", out)
	}
}

func TestContentIsReadAsTextOrParts(t *testing.T) {
	var m []message
	raw := `[{"role":"user","content":"plain"},{"role":"user","content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]},{"role":"assistant","content":null}]`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}

	if m[0].Content != "plain" || m[1].Content != "one\ntwo" || m[2].Content != "" {
		t.Fatalf("read %q, %q and %q", m[0].Content, m[1].Content, m[2].Content)
	}
}

func TestStepSurvivesTheCallID(t *testing.T) {
	for _, step := range []string{"ask", "task-database", "serve-again", "create-web"} {
		if got := stepOf(callID(step)); got != step {
			t.Errorf("stepOf(callID(%q)) = %q", step, got)
		}
	}
	if got := stepOf("call_abc123"); got != "" && got != "abc123" {
		t.Errorf("stepOf of someone else's ID = %q", got)
	}
}

func TestRequestWithABadToolSchemaIsRefused(t *testing.T) {
	for name, parameters := range map[string]string{
		"required is null":           `{"type":"object","properties":{},"required":null}`,
		"required names no property": `{"type":"object","properties":{"a":{"type":"string"}},"required":["b"]}`,
		"not an object":              `{"type":"string"}`,
	} {
		req := request{Messages: []message{{Role: "user", Content: "hello"}}, Tools: offered("broken")}
		req.Tools[0].Function.Parameters = json.RawMessage(parameters)

		if checkRequest(&req) == nil {
			t.Errorf("%s: the request was accepted", name)
		}
	}

	good := request{Messages: []message{{Role: "user", Content: "hello"}}, Tools: assistantTools}
	if err := checkRequest(&good); err != nil {
		t.Errorf("a good request was refused: %v", err)
	}
}

// TestStreamCarriesTheWholeReply reads a streamed reply back the way a client
// does, and checks nothing was lost in the cutting up.
func TestStreamCarriesTheWholeReply(t *testing.T) {
	s := &server{}
	srv := httptest.NewServer(http.HandlerFunc(s.chatCompletions))
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"model":          "anything",
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
		"messages":       []map[string]any{{"role": "user", "content": "Build me a shop"}},
		"tools":          assistantTools,
	})
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type is %q", got)
	}

	var (
		text      strings.Builder
		name, id  string
		arguments strings.Builder
		finish    string
		usage     bool
		done      bool
	)
	lines := bufio.NewScanner(resp.Body)
	lines.Buffer(make([]byte, 1<<20), 1<<20)
	for lines.Scan() {
		data, ok := strings.CutPrefix(lines.Text(), "data: ")
		if !ok {
			continue
		}
		if data == "[DONE]" {
			done = true
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int          `json:"index"`
						ID       string       `json:"id"`
						Function functionCall `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage map[string]int `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("an event is not JSON: %v: %s", err, data)
		}
		if chunk.Usage != nil {
			usage = true
		}
		for _, choice := range chunk.Choices {
			text.WriteString(choice.Delta.Content)
			for _, tc := range choice.Delta.ToolCalls {
				if tc.ID != "" {
					id = tc.ID
				}
				name += tc.Function.Name
				arguments.WriteString(tc.Function.Arguments)
			}
			if choice.FinishReason != nil {
				finish = *choice.FinishReason
			}
		}
	}

	if !done || !usage || finish != "tool_calls" {
		t.Fatalf("done=%v usage=%v finish=%q", done, usage, finish)
	}
	if text.String() != "Before I design this, one question." {
		t.Fatalf("text came through as %q", text.String())
	}
	if name != "ask_user" || stepOf(id) != "ask" {
		t.Fatalf("tool call came through as %q with ID %q", name, id)
	}

	var args struct {
		Question string `json:"question"`
		Options  []struct {
			Label string `json:"label"`
		} `json:"options"`
	}
	if err := json.Unmarshal([]byte(arguments.String()), &args); err != nil {
		t.Fatalf("the arguments did not join back into JSON: %v: %s", err, arguments.String())
	}
	if args.Question == "" || len(args.Options) != 2 {
		t.Fatalf("arguments came through as %+v", args)
	}
}

func TestPiecesJoinBackIntoTheText(t *testing.T) {
	for _, text := range []string{"", "one", "one two three four five six seven", "a\n\nb  c\n", shopSummary} {
		if got := strings.Join(pieces(text), ""); got != text {
			t.Errorf("pieces(%q) join to %q", text, got)
		}
	}
}

// The API the agent writes must be the API the description describes, or the
// Backend tab shows something that is not there.
func TestServerFileIsWhatTheScriptSaysItIs(t *testing.T) {
	plain, described := serverPy(false), serverPy(true)

	if strings.Contains(plain, "OPENAPI") || strings.Contains(plain, "openapi.json") {
		t.Error("the first version of the server already has a description")
	}
	for _, want := range []string{`OPENAPI = {`, `if path == "/openapi.json":`, `"/api/products/{id}"`, `"securitySchemes"`} {
		if !strings.Contains(described, want) {
			t.Errorf("the described server lacks %s", want)
		}
	}
	if strings.Contains(described, "(no description") || strings.Contains(described, "(the description is not served") {
		t.Error("a placeholder was left in the described server")
	}
}

// A tool is sent only the arguments it says it takes: the coding agent's
// shell tool takes a description in some versions and not in others.
func TestToolIsGivenOnlyTheArgumentsItTakes(t *testing.T) {
	tools := offered("bash", "write")
	tools[0].Function.Parameters = json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout":{"type":"integer"}},"required":["command"]}`)

	out := agentReply(conversationOf(&request{Messages: []message{taskFor("ci", "ci", "Check the API")}, Tools: tools}))
	if len(out.Calls) != 1 || out.Calls[0].Name != "bash" {
		t.Fatalf("asked for %v, want bash", names(out))
	}
	if _, sent := out.Calls[0].Args["description"]; sent {
		t.Error("sent a description to a tool that takes none")
	}
	if out.Calls[0].Args["command"] == "" {
		t.Error("the command was dropped")
	}
}

func TestAnAgentAnswersTheOwner(t *testing.T) {
	text := "The owner wrote to you directly, in the project's chat:\n\n@tom make a mockup of the design\n\nFiles the owner gave for this, now on this machine:\n- /home/coder/.coder/attachments/ab12cd34/logo.png (image/png, 2 KB)\n"
	var asked map[string]any
	_, replies := play(t, agentReply, agentTools, []message{taskFor("storefront", "web", text)}, func(tc toolCall) string {
		switch tc.Function.Name {
		case "bash":
			return "-rw-r--r-- 1 coder coder 2048 Oct 15 12:00 /home/coder/.coder/attachments/ab12cd34/logo.png"
		case "team_ask_owner":
			json.Unmarshal([]byte(tc.Function.Arguments), &asked)
			return "Your question is in the chat, under your name (tom). The owner's answer comes to you later."
		}
		return "ok"
	}, 10)

	if asked["agent_name"] != "tom" || !strings.Contains(fmt.Sprint(asked["files"]), "/home/coder/mockups/minimal.svg") {
		t.Errorf("ask_owner was called with %v", asked)
	}
	report := replies[len(replies)-1].Text
	for _, want := range []string{"I'm tom, the storefront agent on web.", "attachments/ab12cd34/logo.png", "asked you to choose"} {
		if !strings.Contains(report, want) {
			t.Errorf("the report %q does not say %q", report, want)
		}
	}
}
