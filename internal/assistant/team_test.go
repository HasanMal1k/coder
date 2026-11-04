package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// teamOf sets up two machines with an agent on each, one of which has
// finished a task and left a note, and returns the team's record.
func teamOf(t *testing.T) (*harness, *Team) {
	t.Helper()
	h := newHarness(t, func(int, []Message) (Message, error) { return says("hi"), nil })
	ctx := context.Background()
	id := h.chat("alice")

	h.machines.Create(ctx, "alice", MachineSpec{Name: "api"}, func(string) {})
	h.machines.Create(ctx, "alice", MachineSpec{Name: "web"}, func(string) {})
	h.machines.Create(ctx, "mallory", MachineSpec{Name: "secret"}, func(string) {})

	backend, _ := h.agents.Ensure(ctx, "alice", id, "id-api", "backend", "h-backend")
	h.agents.Begin(ctx, backend.ID, "Build a JSON API on port 8000.", false)
	h.agents.SetActivity(ctx, backend.ID, "Writing api/server.py")
	h.agents.SetActivity(ctx, backend.ID, "Running python3 server.py")
	h.agents.Finish(ctx, backend.ID, AgentIdle, "Built /home/coder/api/server.py, listening on port 8000.")

	frontend, _ := h.agents.Ensure(ctx, "alice", id, "id-web", "frontend", "h-frontend")
	h.agents.Begin(ctx, frontend.ID, "Build a page that shows the API's data.", true)
	h.agents.SetActivity(ctx, frontend.ID, "Reading up on backend on api")

	other, _ := h.agents.Ensure(ctx, "mallory", h.chat("mallory"), "id-secret", "spy", "h-spy")
	h.agents.Begin(ctx, other.ID, "Something private.", false)

	if err := h.svc.ShareNote(ctx, "alice", "api", "backend", "API endpoints", "GET / returns {service, message}."); err != nil {
		t.Fatalf("ShareNote: %v", err)
	}
	h.svc.ShareNote(ctx, "mallory", "secret", "spy", "Private", "Nobody else should read this.")

	team, err := h.svc.Team(ctx, "alice")
	if err != nil {
		t.Fatalf("Team: %v", err)
	}
	return h, team
}

func TestTheTeamIsEverythingOneUsersAgentsKnow(t *testing.T) {
	_, team := teamOf(t)

	if len(team.Machines) != 2 || len(team.Agents) != 2 || len(team.Notes) != 1 {
		t.Fatalf("team has %d machines, %d agents, %d notes; want alice's 2, 2 and 1 and nobody else's", len(team.Machines), len(team.Agents), len(team.Notes))
	}

	backend := team.Agent("api", "backend")
	if backend == nil || backend.Status != AgentIdle || len(backend.Tasks) != 1 {
		t.Fatalf("backend = %+v", backend)
	}
	task := backend.Tasks[0]
	if task.Status != "done" || task.Task != "Build a JSON API on port 8000." || !strings.Contains(task.Report, "port 8000") {
		t.Errorf("backend's task = %+v", task)
	}
	if !equal(task.Log, []string{"Writing api/server.py", "Running python3 server.py"}) {
		t.Errorf("backend's log = %q, want each step it took", task.Log)
	}

	frontend := team.Agent("web", "frontend")
	if frontend == nil || frontend.Status != AgentWorking || frontend.Activity != "Reading up on backend on api" {
		t.Errorf("frontend = %+v, want it shown working, with what it is doing", frontend)
	}
	if team.Agent("secret", "spy") != nil || team.Agent("api", "frontend") != nil {
		t.Error("found an agent that isn't there, or isn't this user's")
	}
}

func TestTheTeamAsFilesForAMachine(t *testing.T) {
	_, team := teamOf(t)
	files := team.Files("web")

	for _, path := range []string{"TEAM.md", "NOTES.md", "team.json", "agents/api--backend.md", "agents/web--frontend.md"} {
		if files[path] == "" {
			t.Errorf("no %s among the files: %v", path, keys(files))
		}
	}
	for _, path := range []string{"ARCHITECTURE.md", "CONNECTIONS.md"} {
		if files[path] == "" {
			t.Errorf("no %s among the files: %v", path, keys(files))
		}
	}
	if _, ok := files["connections.env"]; !ok || len(files) != 8 {
		t.Errorf("files = %v", keys(files))
	}

	overview := files["TEAM.md"]
	for _, want := range []string{
		"This machine is `web`",
		"`api`: running",
		"http://api:<port>",
		"### h-backend (backend) on `api`",
		"Latest task (done): Build a JSON API on port 8000.",
		"listening on port 8000",
		"agents/api--backend.md",
		"### h-frontend (frontend) on `web`",
		"Right now: Reading up on backend on api",
		"API endpoints (from backend on `api`)",
	} {
		if !strings.Contains(overview, want) {
			t.Errorf("TEAM.md does not mention %q:\n%s", want, overview)
		}
	}
	if strings.Contains(overview, "secret") || strings.Contains(overview, "spy") {
		t.Error("TEAM.md mentions another user's machine or agent")
	}

	detail := files["agents/api--backend.md"]
	for _, want := range []string{"## Task 1 (done)", "### What it was asked", "- Writing api/server.py", "### Its report", "Built /home/coder/api/server.py"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the agent's file does not contain %q:\n%s", want, detail)
		}
	}

	if !strings.Contains(files["NOTES.md"], "GET / returns {service, message}.") || strings.Contains(files["NOTES.md"], "Nobody else") {
		t.Errorf("NOTES.md = %q", files["NOTES.md"])
	}

	var decoded Team
	if err := json.Unmarshal([]byte(files["team.json"]), &decoded); err != nil || len(decoded.Agents) != 2 {
		t.Errorf("team.json does not hold the record: %v", err)
	}

	// The same record for another machine differs only in saying which
	// machine it is, so an unchanged team writes nothing new anywhere.
	again := team.Files("web")
	for path := range files {
		if files[path] != again[path] {
			t.Errorf("%s is not the same when produced twice", path)
		}
	}
}

func TestNotesAreChecked(t *testing.T) {
	h, _ := teamOf(t)
	ctx := context.Background()

	for name, note := range map[string][3]string{
		"no author":     {"", "topic", "text"},
		"no topic":      {"backend", "  ", "text"},
		"no text":       {"backend", "topic", ""},
		"long topic":    {"backend", strings.Repeat("x", maxNoteTopic+1), "text"},
		"too much text": {"backend", "topic", strings.Repeat("x", maxNoteBody+1)},
	} {
		if err := h.svc.ShareNote(ctx, "alice", "api", note[0], note[1], note[2]); !errors.Is(err, ErrInvalidNote) {
			t.Errorf("%s: %v, want ErrInvalidNote", name, err)
		}
	}

	team, _ := h.svc.Team(ctx, "alice")
	if len(team.Notes) != 1 {
		t.Errorf("%d notes after only invalid ones were added, want the 1 from before", len(team.Notes))
	}
	if got := team.NotesText("endpoints"); !strings.Contains(got, "GET / returns") {
		t.Errorf("notes about endpoints = %q", got)
	}
	if got := team.NotesText("database"); !strings.Contains(got, `No note has a topic containing "database"`) {
		t.Errorf("notes about something nobody wrote of = %q", got)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
