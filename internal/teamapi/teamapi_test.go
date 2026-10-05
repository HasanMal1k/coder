package teamapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HasanMal1k/coder/internal/assistant"
	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/store"
)

const (
	machineID  = "0b0f3a4e-7c1d-4f7e-9a53-2f6f0c7d9e11"
	machineKey = machineID + ".guest-token"
)

type fakeGuests struct{}

func (fakeGuests) AuthenticateGuest(_ context.Context, id, token string) error {
	if id != machineID || token != "guest-token" {
		return instance.ErrGuestUnauthorized
	}
	return nil
}

func (fakeGuests) ForMachine(_ context.Context, id string) (*store.Instance, error) {
	return &store.Instance{ID: id, OwnerID: "alice", Name: "web"}, nil
}

// fakeTeams holds one user's team.
type noted struct {
	instanceID string
	started    map[string]int64
}

type fakeTeams struct {
	team     assistant.Team
	notes    []string
	asked    []string
	services []string
	// noted is each time a machine asked for its record, and what it said
	// it was running.
	noted []noted
}

func (f *fakeTeams) Team(_ context.Context, ownerID string) (*assistant.Team, error) {
	if ownerID != "alice" {
		return &assistant.Team{}, nil
	}
	copied := f.team
	return &copied, nil
}

func (f *fakeTeams) ShareNote(_ context.Context, ownerID, machine, author, topic, body string) error {
	if strings.TrimSpace(body) == "" {
		return assistant.ErrInvalidNote
	}
	f.notes = append(f.notes, ownerID+"|"+machine+"|"+author+"|"+topic+"|"+body)
	return nil
}

func (f *fakeTeams) MachineRecord(ctx context.Context, machine *store.Instance) (*assistant.MachineRecord, error) {
	team, _ := f.Team(ctx, machine.OwnerID)
	return &assistant.MachineRecord{
		Files:    team.Files(machine.Name),
		Services: []assistant.GuestService{{Name: "web", Command: "npm start", Directory: "/home/coder/web", Port: 3000}},
	}, nil
}

func (f *fakeTeams) NoteGuest(instanceID string, started map[string]int64) {
	f.noted = append(f.noted, noted{instanceID, started})
}

func (f *fakeTeams) AskAgent(_ context.Context, ownerID, fromMachine, fromAgent, toMachine, toAgent, question string) (string, error) {
	f.asked = append(f.asked, fromAgent+"@"+fromMachine+" -> "+toAgent+"@"+toMachine+": "+question)
	if toAgent == "nobody" {
		return "", assistant.Mistake("There is no agent called \"nobody\".")
	}
	return "It listens on port 8000.", nil
}

func (f *fakeTeams) RunService(_ context.Context, machine *store.Instance, name, command, directory string, port int) (string, error) {
	f.services = append(f.services, fmt.Sprintf("%s|%s|%s|%s|%d", machine.Name, name, command, directory, port))
	return "It is up and answering.", nil
}

func (f *fakeTeams) StopService(_ context.Context, machine *store.Instance, name string) (string, error) {
	if name != "web" {
		return "", assistant.Mistake("There is no service called that.")
	}
	return "Stopped.", nil
}

func newServer(t *testing.T) (*httptest.Server, *fakeTeams) {
	t.Helper()
	started := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

	teams := &fakeTeams{team: assistant.Team{
		Machines: []assistant.TeamMachine{
			{Name: "api", Status: "running", CPUs: 1, MemoryMB: 1024, DiskGB: 10},
			{Name: "web", Status: "running", CPUs: 2, MemoryMB: 2048, DiskGB: 20},
		},
		Agents: []assistant.TeamAgent{{
			Name: "backend", Machine: "api", Project: "A website", Status: "idle",
			Tasks: []assistant.TeamTask{{
				Task: "Build a JSON API on port 8000.", Status: "done",
				Log: []string{"Writing api/server.py"}, Report: "Listening on port 8000.", StartedAt: started,
			}},
		}},
		Notes: []assistant.TeamNote{{Machine: "api", Author: "backend", Topic: "API endpoints", Body: "GET / returns JSON.", CreatedAt: started}},
	}}

	h := New(fakeGuests{}, teams)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /guest/team", h.Files)
	mux.HandleFunc("POST /mcp", h.MCP)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server, teams
}

// rpc sends one MCP message and returns the status and the decoded answer.
func rpc(t *testing.T, server *httptest.Server, key, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call MCP: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("answer is not JSON: %s", raw)
		}
	}
	return resp.StatusCode, out
}

// callTool calls a tool and returns its text and whether it was an error.
func callTool(t *testing.T, server *httptest.Server, name string, args map[string]string) (string, bool) {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	status, out := rpc(t, server, machineKey, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":`+string(params)+`}`)
	if status != 200 || out["error"] != nil {
		t.Fatalf("tools/call %s: %d %v", name, status, out)
	}
	result := out["result"].(map[string]any)
	content := result["content"].([]any)[0].(map[string]any)
	if content["type"] != "text" {
		t.Errorf("content type = %v", content["type"])
	}
	isError, _ := result["isError"].(bool)
	return content["text"].(string), isError
}

func TestInitialize(t *testing.T) {
	server, _ := newServer(t)

	for asked, want := range map[string]string{
		"2025-06-18": "2025-06-18", // one we speak: agreed to
		"2024-11-05": "2024-11-05",
		"2099-01-01": protocolVersions[0], // one we don't: our newest is offered
	} {
		status, out := rpc(t, server, machineKey, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+asked+`","capabilities":{},"clientInfo":{"name":"opencode","version":"1"}}}`)
		result, _ := out["result"].(map[string]any)
		if status != 200 || result["protocolVersion"] != want {
			t.Errorf("initialize asking for %s: %d %v, want version %s", asked, status, out, want)
		}
		if out["id"] != float64(1) || out["jsonrpc"] != "2.0" {
			t.Errorf("envelope = %v", out)
		}
		if caps, _ := result["capabilities"].(map[string]any); caps["tools"] == nil {
			t.Errorf("capabilities = %v, want tools", result["capabilities"])
		}
	}
}

func TestNotificationsGetNoAnswer(t *testing.T) {
	server, _ := newServer(t)

	status, out := rpc(t, server, machineKey, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if status != http.StatusAccepted || out != nil {
		t.Errorf("notification answered %d %v, want 202 and nothing", status, out)
	}
}

func TestToolsAreListedWithSchemas(t *testing.T) {
	server, _ := newServer(t)

	_, out := rpc(t, server, machineKey, `{"jsonrpc":"2.0","id":"abc","method":"tools/list"}`)
	if out["id"] != "abc" {
		t.Errorf("id = %v, want it echoed as it was sent", out["id"])
	}

	listed := out["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, raw := range listed {
		tool := raw.(map[string]any)
		names[tool["name"].(string)] = true
		schema, _ := tool["inputSchema"].(map[string]any)
		if schema["type"] != "object" || schema["properties"] == nil || tool["description"] == "" {
			t.Errorf("tool %v is not fully described: %v", tool["name"], tool)
		}
		if _, isList := schema["required"].([]any); !isList {
			t.Errorf("tool %v: required = %v, want a list (clients reject null)", tool["name"], schema["required"])
		}
	}
	for _, want := range []string{"list_machines", "list_agents", "get_agent", "get_architecture", "ask_agent", "run_service", "stop_service", "read_notes", "share_note"} {
		if !names[want] {
			t.Errorf("no tool called %s", want)
		}
	}
}

func TestAnAgentCanLearnAboutTheOthers(t *testing.T) {
	server, _ := newServer(t)

	machines, _ := callTool(t, server, "list_machines", nil)
	if !strings.Contains(machines, `You are on "web"`) || !strings.Contains(machines, "http://api:<port>") {
		t.Errorf("list_machines = %q", machines)
	}

	agents, _ := callTool(t, server, "list_agents", nil)
	if !strings.Contains(agents, "backend on api: idle") || !strings.Contains(agents, "Build a JSON API on port 8000.") {
		t.Errorf("list_agents = %q", agents)
	}
	if here, _ := callTool(t, server, "list_agents", map[string]string{"machine": "web"}); !strings.Contains(here, `no agents on "web"`) {
		t.Errorf("list_agents on a machine with none = %q", here)
	}

	detail, isError := callTool(t, server, "get_agent", map[string]string{"machine": "api", "agent": "backend"})
	if isError {
		t.Fatalf("get_agent failed: %s", detail)
	}
	for _, want := range []string{"What it was asked", "Build a JSON API on port 8000.", "- Writing api/server.py", "Listening on port 8000."} {
		if !strings.Contains(detail, want) {
			t.Errorf("get_agent does not contain %q:\n%s", want, detail)
		}
	}

	// Asking for an agent that isn't there is the agent's mistake to read
	// and correct, with the ones there are to choose from.
	missing, isError := callTool(t, server, "get_agent", map[string]string{"machine": "api", "agent": "nobody"})
	if !isError || !strings.Contains(missing, `no agent called "nobody"`) || !strings.Contains(missing, "backend on api") {
		t.Errorf("get_agent for nobody = %q (error: %v)", missing, isError)
	}

	notes, _ := callTool(t, server, "read_notes", nil)
	if !strings.Contains(notes, "GET / returns JSON.") {
		t.Errorf("read_notes = %q", notes)
	}
}

func TestAnAgentCanLeaveANote(t *testing.T) {
	server, teams := newServer(t)

	said, isError := callTool(t, server, "share_note", map[string]string{"agent": "frontend", "topic": "Page", "text": "Served on port 3000."})
	if isError || !strings.Contains(said, "Noted") {
		t.Errorf("share_note = %q (error: %v)", said, isError)
	}
	// Whose note it is and where it came from are the server's to say, from
	// who was authenticated; the agent only supplies its own name.
	if len(teams.notes) != 1 || teams.notes[0] != "alice|web|frontend|Page|Served on port 3000." {
		t.Errorf("notes recorded = %v", teams.notes)
	}

	if said, isError := callTool(t, server, "share_note", map[string]string{"agent": "frontend", "topic": "Empty"}); !isError || !strings.Contains(said, "A note needs") {
		t.Errorf("an empty note = %q (error: %v)", said, isError)
	}
}

func TestAgentsAskEachOtherAndKeepProgramsRunning(t *testing.T) {
	server, teams := newServer(t)

	answer, isError := callTool(t, server, "ask_agent", map[string]string{"agent_name": "frontend", "machine": "api", "agent": "backend", "question": "Which port?"})
	if isError || answer != "It listens on port 8000." {
		t.Errorf("ask_agent = %q (error: %v)", answer, isError)
	}
	// Which machine the question came from is the server's to say.
	if len(teams.asked) != 1 || teams.asked[0] != "frontend@web -> backend@api: Which port?" {
		t.Errorf("asked = %v", teams.asked)
	}
	if said, isError := callTool(t, server, "ask_agent", map[string]string{"agent_name": "frontend", "machine": "api", "agent": "nobody", "question": "?"}); !isError || !strings.Contains(said, "no agent called") {
		t.Errorf("asking nobody = %q (error: %v)", said, isError)
	}

	// A port given as a number, which is how models tend to give one.
	status, out := rpc(t, server, machineKey, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run_service","arguments":{"name":"web","command":"npm start","directory":"web","port":3000}}}`)
	if status != 200 || out["error"] != nil || len(teams.services) != 1 || teams.services[0] != "web|web|npm start|web|3000" {
		t.Errorf("run_service: %d %v; registered %v", status, out, teams.services)
	}
	if said, isError := callTool(t, server, "run_service", map[string]string{"name": "web", "command": "npm start", "port": "three thousand"}); !isError || !strings.Contains(said, "must be a number") {
		t.Errorf("a port that is not a number = %q (error: %v)", said, isError)
	}

	if said, isError := callTool(t, server, "stop_service", map[string]string{"name": "web"}); isError || said != "Stopped." {
		t.Errorf("stop_service = %q (error: %v)", said, isError)
	}
	if _, isError := callTool(t, server, "stop_service", map[string]string{"name": "ghost"}); !isError {
		t.Error("stopping a service that isn't there was not reported as a mistake")
	}

	design, _ := callTool(t, server, "get_architecture", nil)
	if !strings.Contains(design, "# Architecture") || !strings.Contains(design, "# Connections of `web`") {
		t.Errorf("get_architecture = %q", design)
	}
}

func TestTheRecordSaysWhatToRun(t *testing.T) {
	server, _ := newServer(t)

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/guest/team", nil)
	req.Header.Set("Authorization", "Bearer "+machineKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var record assistant.MachineRecord
	json.NewDecoder(resp.Body).Decode(&record)
	if len(record.Services) != 1 || record.Services[0].Command != "npm start" || record.Services[0].Port != 3000 {
		t.Errorf("services = %+v", record.Services)
	}
}

func TestProtocolErrors(t *testing.T) {
	server, _ := newServer(t)

	code := func(out map[string]any) float64 {
		e, _ := out["error"].(map[string]any)
		c, _ := e["code"].(float64)
		return c
	}

	if _, out := rpc(t, server, machineKey, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`); code(out) != codeMethodNotFound {
		t.Errorf("unknown method: %v", out)
	}
	if _, out := rpc(t, server, machineKey, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_everything","arguments":{}}}`); code(out) != codeInvalidParams {
		t.Errorf("unknown tool: %v", out)
	}
	if status, out := rpc(t, server, machineKey, `not json`); status != 400 || code(out) != codeParseError {
		t.Errorf("not JSON: %d %v", status, out)
	}
	if _, out := rpc(t, server, machineKey, `{"jsonrpc":"1.0","id":1,"method":"ping"}`); code(out) != codeInvalidRequest {
		t.Errorf("wrong jsonrpc version: %v", out)
	}
	if _, out := rpc(t, server, machineKey, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); out["result"] == nil || out["error"] != nil {
		t.Errorf("ping: %v", out)
	}
}

func TestOnlyAMachineMayAsk(t *testing.T) {
	server, _ := newServer(t)

	for name, key := range map[string]string{"no credentials": "", "wrong token": machineID + ".nope", "not a machine": "sk-something"} {
		if status, _ := rpc(t, server, key, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); status != http.StatusUnauthorized {
			t.Errorf("MCP with %s: %d, want 401", name, status)
		}

		req, _ := http.NewRequest(http.MethodGet, server.URL+"/guest/team", nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("files with %s: %d, want 401", name, resp.StatusCode)
		}
	}
}

func TestTheRecordAsFiles(t *testing.T) {
	server, teams := newServer(t)

	get := func(etag string) (*http.Response, assistant.MachineRecord) {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/guest/team", nil)
		req.Header.Set("Authorization", "Bearer "+machineKey)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get files: %v", err)
		}
		defer resp.Body.Close()
		var out assistant.MachineRecord
		json.NewDecoder(resp.Body).Decode(&out)
		return resp, out
	}

	resp, out := get("")
	if resp.StatusCode != 200 || !strings.Contains(out.Files["TEAM.md"], "This machine is `web`") || out.Files["agents/api--backend.md"] == "" {
		t.Fatalf("files = %d %v", resp.StatusCode, out.Files)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}

	if resp, _ := get(etag); resp.StatusCode != http.StatusNotModified {
		t.Errorf("an unchanged record asked for again: %d, want 304", resp.StatusCode)
	}

	teams.team.Agents[0].Status = "working"
	resp, out = get(etag)
	if resp.StatusCode != 200 || resp.Header.Get("ETag") == etag || !strings.Contains(out.Files["TEAM.md"], "Status: working") {
		t.Errorf("a changed record: %d, ETag changed = %v", resp.StatusCode, resp.Header.Get("ETag") != etag)
	}
}

func TestAMachineSaysWhatItIsRunningWhenItAsks(t *testing.T) {
	server, teams := newServer(t)

	ask := func(said string) {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/guest/team", nil)
		req.Header.Set("Authorization", "Bearer "+machineKey)
		if said != "" {
			req.Header.Set("X-Coder-Services", said)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get files: %v", err)
		}
		resp.Body.Close()
	}

	ask(`{"api":42,"git":7}`)
	ask(`{}`)
	ask("")
	ask("not json")

	if len(teams.noted) != 4 {
		t.Fatalf("noted %d times, want once for every time the machine asked", len(teams.noted))
	}
	if n := teams.noted[0]; n.started["api"] != 42 || n.started["git"] != 7 {
		t.Errorf("what it said it runs = %v", n.started)
	}
	if n := teams.noted[1]; n.started == nil || len(n.started) != 0 {
		t.Errorf("a machine running nothing = %v, want an empty list: it does report", n.started)
	}
	if n := teams.noted[2]; n.started != nil {
		t.Errorf("a machine that says nothing = %v, want nil: its daemon is from before it could", n.started)
	}
	if n := teams.noted[3]; n.started == nil || len(n.started) != 0 {
		t.Errorf("a machine that says something unreadable = %v, want an empty list", n.started)
	}

	// Someone who is not a machine is not noted.
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/guest/team", nil)
	req.Header.Set("Authorization", "Bearer nonsense")
	req.Header.Set("X-Coder-Services", `{"api":1}`)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if len(teams.noted) != 4 {
		t.Error("a caller without a machine's credentials was recorded as one")
	}
}
