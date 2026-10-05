package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// eventually waits for cond to hold, failing the test if it doesn't soon.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// lastText returns the text of the chat's latest assistant event.
func (h *harness) lastText(owner, chatID string) string {
	var text string
	for _, raw := range h.payloads(owner, chatID, KindAssistant) {
		var p AssistantPayload
		json.Unmarshal(raw, &p)
		text = p.Text
	}
	return text
}

func background(machine, agent, task string) map[string]any {
	return map[string]any{"machine": machine, "agent": agent, "task": task, "background": true}
}

// isNews reports whether a message is the platform passing on news from a
// background agent.
func isNews(m Message) bool {
	return m.Role == "user" && strings.HasPrefix(m.Content, "[From the platform")
}

func TestABackgroundAgentWorksWhileTheChatIsFree(t *testing.T) {
	var news Message
	h := newHarness(t, func(call int, messages []Message) (Message, error) {
		last := messages[len(messages)-1]
		switch {
		case isNews(last):
			news = last
			return says("The backend is done."), nil
		case call == 0:
			return wants(callTool("c1", toolCodeOnMachine, background("api", "backend", "build it"))), nil
		case last.Role == "tool":
			return says("Started. I'll tell you when it's done."), nil
		default:
			return says("Still going."), nil
		}
	})
	h.machines.Create(context.Background(), "alice", MachineSpec{Name: "api"}, func(string) {})

	release := make(chan struct{})
	h.machines.runTask = func(ctx context.Context, _, _ string, progress func(string)) (string, error) {
		progress("Writing main.go")
		<-release
		return "Built the API on port 8000.", nil
	}

	id := h.chat("alice")
	h.send("alice", id, "build a backend")
	if status := h.settle("alice", id); status != StatusIdle {
		t.Fatalf("status = %q; the chat should be free while the agent works", status)
	}

	agent := h.agents.byName(id, "backend")
	if agent == nil || agent.Status != AgentWorking || !agent.Background || agent.Task != "build it" {
		t.Fatalf("agent = %+v, want it working in the background", agent)
	}
	eventually(t, "the agent's activity", func() bool { return h.agents.byName(id, "backend").Activity == "Writing main.go" })

	// The user can talk to the assistant meanwhile.
	h.send("alice", id, "how's it going?")
	h.settle("alice", id)
	if got := h.lastText("alice", id); got != "Still going." {
		t.Fatalf("reply while the agent works = %q", got)
	}

	close(release)
	eventually(t, "the assistant to react to the agent finishing", func() bool {
		return h.lastText("alice", id) == "The backend is done."
	})
	h.settle("alice", id)

	if !strings.Contains(news.Content, `Background agent "backend" on machine "api" has finished`) || !strings.Contains(news.Content, "Built the API on port 8000.") {
		t.Errorf("the model was told %q", news.Content)
	}
	if agent := h.agents.byName(id, "backend"); agent.Status != AgentIdle || agent.Report != "Built the API on port 8000." {
		t.Errorf("agent after finishing = %+v", agent)
	}

	var finished AgentPayload
	json.Unmarshal(h.payloads("alice", id, KindAgent)[0], &finished)
	if finished.ToolCallID != "c1" || finished.Agent != "backend" || finished.Machine.Name != "api" || finished.Failed {
		t.Errorf("agent event = %+v; it must point back at the call that started the agent", finished)
	}
}

func TestAnAgentDoesOneThingAtATimeButAgentsWorkTogether(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			return wants(
				callTool("a", toolCodeOnMachine, map[string]any{"machine": "api", "agent": "backend", "task": "one"}),
				callTool("b", toolCodeOnMachine, map[string]any{"machine": "api", "agent": "backend", "task": "two"}),
				callTool("c", toolCodeOnMachine, map[string]any{"machine": "api", "agent": "tests", "task": "three"}),
			), nil
		}
		return says("ok"), nil
	})
	h.machines.Create(context.Background(), "alice", MachineSpec{Name: "api"}, func(string) {})

	// Every task that gets to run says so, then holds on until released.
	started := make(chan string, 3)
	release := make(chan struct{})
	h.machines.runTask = func(ctx context.Context, _, task string, _ func(string)) (string, error) {
		started <- task
		<-release
		return "did " + task, nil
	}

	id := h.chat("alice")
	h.send("alice", id, "go")

	// Two agents at work at the same moment.
	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("two agents never worked at the same time")
		}
	}
	// One of the two tasks for "backend" is turned away while the other
	// holds the agent; nothing else starts.
	eventually(t, "a task to be turned away", func() bool {
		for _, raw := range h.payloads("alice", id, KindTool) {
			if strings.Contains(string(raw), "still working on its last task") {
				return true
			}
		}
		return false
	})
	close(release)
	h.settle("alice", id)
	if len(started) != 0 {
		t.Errorf("a third task ran: %q", <-started)
	}

	results := map[string]ToolPayload{}
	for _, raw := range h.payloads("alice", id, KindTool) {
		var p ToolPayload
		json.Unmarshal(raw, &p)
		results[p.ToolCallID] = p
	}

	// Of the two tasks for "backend", one ran and one was turned away.
	turnedAway := 0
	for _, id := range []string{"a", "b"} {
		if r := results[id]; r.Failed {
			turnedAway++
			if !strings.Contains(r.Content, "still working on its last task") {
				t.Errorf("%s: %q", id, r.Content)
			}
		}
	}
	if turnedAway != 1 {
		t.Errorf("%d of the two tasks for one agent were turned away, want exactly 1: %+v", turnedAway, results)
	}
	if r := results["c"]; r.Failed || r.Content != "did three" {
		t.Errorf("the second agent's task = %+v", r)
	}
}

func TestStoppingABackgroundAgent(t *testing.T) {
	var news Message
	h := newHarness(t, func(call int, messages []Message) (Message, error) {
		last := messages[len(messages)-1]
		switch {
		case isNews(last):
			news = last
			return says("Noted."), nil
		case call == 0:
			return wants(callTool("c1", toolCodeOnMachine, background("api", "backend", "long job"))), nil
		default:
			return says("Started."), nil
		}
	})
	h.machines.Create(context.Background(), "alice", MachineSpec{Name: "api"}, func(string) {})

	working := make(chan struct{})
	h.machines.runTask = func(ctx context.Context, _, _ string, _ func(string)) (string, error) {
		close(working)
		<-ctx.Done()
		return "", ctx.Err()
	}

	id := h.chat("alice")
	h.send("alice", id, "go")
	h.settle("alice", id)
	<-working

	agent := h.agents.byName(id, "backend")
	if err := h.svc.StopAgent(context.Background(), "mallory", agent.ID); !errors.Is(err, ErrAgentNotFound) {
		t.Errorf("StopAgent as another user: %v, want ErrAgentNotFound", err)
	}
	if err := h.svc.StopAgent(context.Background(), "alice", "not-an-id"); !errors.Is(err, ErrAgentNotFound) {
		t.Errorf("StopAgent with a malformed ID: %v, want ErrAgentNotFound", err)
	}
	if err := h.svc.StopAgent(context.Background(), "alice", agent.ID); err != nil {
		t.Fatalf("StopAgent: %v", err)
	}

	eventually(t, "the assistant to hear the agent was stopped", func() bool { return h.lastText("alice", id) == "Noted." })
	if got := h.agents.byName(id, "backend").Status; got != AgentStopped {
		t.Errorf("agent status = %q, want stopped", got)
	}
	if !strings.Contains(news.Content, `The user stopped background agent "backend"`) {
		t.Errorf("the model was told %q", news.Content)
	}
}

func TestStoppingAnAgentTheAssistantIsWaitingFor(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			return wants(callTool("c1", toolCodeOnMachine, map[string]any{"machine": "api", "agent": "backend", "task": "long"})), nil
		}
		return says("Understood, I've left it."), nil
	})
	h.machines.Create(context.Background(), "alice", MachineSpec{Name: "api"}, func(string) {})

	working := make(chan struct{})
	h.machines.runTask = func(ctx context.Context, _, _ string, _ func(string)) (string, error) {
		close(working)
		<-ctx.Done()
		return "", ctx.Err()
	}

	id := h.chat("alice")
	h.send("alice", id, "go")
	<-working

	if err := h.svc.StopAgent(context.Background(), "alice", h.agents.byName(id, "backend").ID); err != nil {
		t.Fatalf("StopAgent: %v", err)
	}
	if status := h.settle("alice", id); status != StatusIdle {
		t.Errorf("status = %q", status)
	}

	// The task ended, the run did not: the model is told, and carries on.
	var result ToolPayload
	json.Unmarshal(h.payloads("alice", id, KindTool)[0], &result)
	if !result.Failed || !strings.Contains(result.Content, "the user stopped this agent") {
		t.Errorf("result = %+v", result)
	}
	if got := h.lastText("alice", id); got != "Understood, I've left it." {
		t.Errorf("last reply = %q; stopping one agent must not stop the conversation", got)
	}
	if got := h.kinds("alice", id); strings.Contains(strings.Join(got, ","), KindError) {
		t.Errorf("events = %v; nothing failed", got)
	}
}

func TestNewsThatArrivesWhileTheModelWritesIsNotMissed(t *testing.T) {
	writing := make(chan struct{})
	finishWriting := make(chan struct{})
	var heard bool

	h := newHarness(t, func(call int, messages []Message) (Message, error) {
		last := messages[len(messages)-1]
		switch {
		case isNews(last):
			heard = true
			return says("Heard it."), nil
		case call == 0:
			return wants(callTool("c1", toolCodeOnMachine, background("api", "backend", "quick"))), nil
		default:
			// The final message of the first run, still being written when
			// the agent finishes.
			close(writing)
			<-finishWriting
			return says("Started."), nil
		}
	})
	h.machines.Create(context.Background(), "alice", MachineSpec{Name: "api"}, func(string) {})

	release := make(chan struct{})
	h.machines.runTask = func(ctx context.Context, _, _ string, _ func(string)) (string, error) {
		<-release
		return "done", nil
	}

	id := h.chat("alice")
	h.send("alice", id, "go")
	<-writing
	close(release)
	eventually(t, "the agent's news to be recorded", func() bool { return len(h.payloads("alice", id, KindAgent)) == 1 })
	close(finishWriting)

	eventually(t, "a run for the news", func() bool { return h.lastText("alice", id) == "Heard it." })
	if !heard {
		t.Error("the model was not shown the news")
	}
}

func TestBackgroundAgentsCanNotKeepTheAssistantGoingForEver(t *testing.T) {
	var (
		mu      sync.Mutex
		started int
	)
	// Every time it hears an agent finished, the model starts another.
	h := newHarness(t, func(_ int, messages []Message) (Message, error) {
		if last := messages[len(messages)-1]; last.Role == "tool" {
			return says("Started another."), nil
		}
		mu.Lock()
		started++
		n := started
		mu.Unlock()
		return wants(callTool(fmt.Sprintf("c%d", n), toolCodeOnMachine, background("api", fmt.Sprintf("agent-%d", n), "again"))), nil
	})
	h.machines.Create(context.Background(), "alice", MachineSpec{Name: "api"}, func(string) {})
	h.machines.runTask = func(context.Context, string, string, func(string)) (string, error) { return "done", nil }

	id := h.chat("alice")
	h.send("alice", id, "go")

	eventually(t, "the assistant to give up", func() bool {
		for _, raw := range h.payloads("alice", id, KindError) {
			if strings.Contains(string(raw), "times in a row") {
				return true
			}
		}
		return false
	})
	h.settle("alice", id)

	// And it stays given up: nothing more is started.
	mu.Lock()
	before := started
	mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	after := started
	mu.Unlock()
	if after != before {
		t.Errorf("agents kept being started after the assistant gave up: %d, then %d", before, after)
	}
	if errs := h.payloads("alice", id, KindError); len(errs) != 1 {
		t.Errorf("%d error events, want the one saying it gave up", len(errs))
	}

	// A word from the user starts things again.
	h.send("alice", id, "carry on")
	eventually(t, "the user's message to be acted on", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return started > after
	})
}

func TestRecoveryFinishesInterruptedAgents(t *testing.T) {
	h := newHarness(t, func(int, []Message) (Message, error) { return says("hi"), nil })
	id := h.chat("alice")
	ctx := context.Background()

	fg, _ := h.agents.Ensure(ctx, "alice", id, "id-api", "foreground")
	bg, _ := h.agents.Ensure(ctx, "alice", id, "id-api", "background")
	h.agents.Begin(ctx, fg.ID, "a", false)
	h.agents.Begin(ctx, bg.ID, "b", true)

	if _, err := h.svc.RecoverInterrupted(ctx); err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}

	for _, name := range []string{"foreground", "background"} {
		if a := h.agents.byName(id, name); a.Status != AgentFailed || !strings.Contains(a.Report, "server restarted") {
			t.Errorf("%s agent = %+v", name, a)
		}
	}
	// Only the background task needs its chat told: a foreground one is
	// covered by the chat's own run being marked as cut off.
	news := h.payloads("alice", id, KindAgent)
	if len(news) != 1 || !strings.Contains(string(news[0]), `"agent":"background"`) {
		t.Errorf("agent events = %s", news)
	}
}

func TestDeletingAChatStopsItsAgents(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			return wants(callTool("c1", toolCodeOnMachine, background("api", "backend", "long"))), nil
		}
		return says("Started."), nil
	})
	h.machines.Create(context.Background(), "alice", MachineSpec{Name: "api"}, func(string) {})

	stopped := make(chan struct{})
	h.machines.runTask = func(ctx context.Context, _, _ string, _ func(string)) (string, error) {
		<-ctx.Done()
		close(stopped)
		return "", ctx.Err()
	}

	id := h.chat("alice")
	h.send("alice", id, "go")
	h.settle("alice", id)

	if err := h.svc.DeleteChat(context.Background(), "alice", id); err != nil {
		t.Fatalf("DeleteChat: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the chat's agent kept working after the chat was deleted")
	}
}

func TestTheModelCanListAndStopAgents(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		switch call {
		case 0:
			return wants(callTool("c1", toolCodeOnMachine, background("api", "backend", "build the API"))), nil
		case 1:
			return wants(callTool("c2", toolListAgents, map[string]any{})), nil
		case 2:
			return wants(callTool("c3", toolStopAgent, map[string]any{"machine": "api", "agent": "backend"})), nil
		case 3:
			return wants(
				callTool("c4", toolStopAgent, map[string]any{"machine": "api", "agent": "nobody"}),
				callTool("c5", toolCodeOnMachine, map[string]any{"machine": "api", "agent": "Not Valid", "task": "x"}),
			), nil
		default:
			return says("ok"), nil
		}
	})
	h.machines.Create(context.Background(), "alice", MachineSpec{Name: "api"}, func(string) {})
	h.machines.runTask = func(ctx context.Context, _, _ string, progress func(string)) (string, error) {
		progress("Writing api.go")
		<-ctx.Done()
		return "", ctx.Err()
	}

	id := h.chat("alice")
	h.send("alice", id, "go")
	eventually(t, "the run to finish", func() bool {
		chat, _ := h.svc.GetChat(context.Background(), "alice", id)
		return chat.Status == StatusIdle && len(h.payloads("alice", id, KindTool)) >= 5
	})

	results := map[string]ToolPayload{}
	for _, raw := range h.payloads("alice", id, KindTool) {
		var p ToolPayload
		json.Unmarshal(raw, &p)
		results[p.ToolCallID] = p
	}

	listing := results["c2"].Content
	for _, want := range []string{`"backend" on api: working`, "Task: build the API"} {
		if !strings.Contains(listing, want) {
			t.Errorf("list_agents = %q, want it to mention %q", listing, want)
		}
	}
	if !strings.Contains(results["c3"].Content, "has been told to stop") {
		t.Errorf("stop_agent = %+v", results["c3"])
	}
	if r := results["c4"]; !r.Failed || !strings.Contains(r.Content, `no agent called "nobody"`) {
		t.Errorf("stopping an agent that doesn't exist = %+v", r)
	}
	if r := results["c5"]; !r.Failed || !strings.Contains(r.Content, "not a valid agent name") {
		t.Errorf("a bad agent name = %+v", r)
	}
	eventually(t, "the agent to be stopped", func() bool { return h.agents.byName(id, "backend").Status == AgentStopped })
}

// twoAgents gives alice a backend agent on api and a frontend agent on web,
// both idle, in one chat.
func twoAgents(t *testing.T) (*harness, string) {
	t.Helper()
	h := newHarness(t, func(int, []Message) (Message, error) { return says("hi"), nil })
	ctx := context.Background()
	id := h.chat("alice")
	h.machines.Create(ctx, "alice", MachineSpec{Name: "api"}, func(string) {})
	h.machines.Create(ctx, "alice", MachineSpec{Name: "web"}, func(string) {})
	h.agents.Ensure(ctx, "alice", id, "id-api", "backend")
	h.agents.Ensure(ctx, "alice", id, "id-web", "frontend")
	return h, id
}

func TestOneAgentAsksAnotherAndGetsItsAnswer(t *testing.T) {
	h, id := twoAgents(t)
	ctx := context.Background()

	var asked string
	h.machines.runTask = func(_ context.Context, name, task string, _ func(string)) (string, error) {
		asked = name + ": " + task
		return "GET /api/orders returns a list of {id, total}. It listens on port 8080.", nil
	}

	answer, err := h.svc.AskAgent(ctx, "alice", "web", "frontend", "api", "backend", "What does /api/orders return?")
	if err != nil {
		t.Fatalf("AskAgent: %v", err)
	}
	if !strings.Contains(answer, "returns a list of {id, total}") {
		t.Errorf("answer = %q", answer)
	}

	// The question went to the other agent, on its own machine, saying who
	// was asking.
	for _, want := range []string{"api: ", `Another agent, "frontend" on machine "web"`, "What does /api/orders return?", "Do not change anything"} {
		if !strings.Contains(asked, want) {
			t.Errorf("the other agent was given %q; want it to contain %q", asked, want)
		}
	}
	if a := h.agents.byName(id, "backend"); a.Status != AgentIdle {
		t.Errorf("backend after answering = %q, want idle again", a.Status)
	}

	// The exchange is in the chat, for the user to see.
	var exchange MessagePayload
	json.Unmarshal(h.payloads("alice", id, KindMessage)[0], &exchange)
	if exchange.From != (Peer{"web", "frontend"}) || exchange.To != (Peer{"api", "backend"}) || !strings.Contains(exchange.Answer, "port 8080") || exchange.Queued {
		t.Errorf("exchange = %+v", exchange)
	}
}

func TestAQuestionForABusyAgentWaitsForIt(t *testing.T) {
	h, id := twoAgents(t)
	ctx := context.Background()

	backend := h.agents.byName(id, "backend")
	h.agents.Begin(ctx, backend.ID, "Build the API.", false)
	h.agents.SetActivity(ctx, backend.ID, "Writing Program.cs")

	h.machines.runTask = func(context.Context, string, string, func(string)) (string, error) {
		t.Error("a busy agent was given the question as a second task")
		return "", nil
	}
	answer, err := h.svc.AskAgent(ctx, "alice", "web", "frontend", "api", "backend", "Which port?")
	if err != nil {
		t.Fatalf("AskAgent: %v", err)
	}
	for _, want := range []string{"in the middle of a task", "left for it", "Writing Program.cs", "Build the API."} {
		if !strings.Contains(answer, want) {
			t.Errorf("answer for a busy agent lacks %q:\n%s", want, answer)
		}
	}

	// It finds the question in front of its next task, once.
	h.agents.Finish(ctx, backend.ID, AgentIdle, "done")
	var tasks []string
	h.machines.runTask = func(_ context.Context, _, task string, _ func(string)) (string, error) {
		tasks = append(tasks, task)
		return "ok", nil
	}
	job := func(task string) job {
		return job{ownerID: "alice", chatID: id, machine: Machine{ID: "id-api", Name: "api"}, agent: backend, task: task}
	}
	h.agents.Begin(ctx, backend.ID, "Add an endpoint.", false)
	h.svc.work(ctx, job("Add an endpoint."))
	h.agents.Begin(ctx, backend.ID, "Add another.", false)
	h.svc.work(ctx, job("Add another."))

	if len(tasks) != 2 || !strings.Contains(tasks[0], `From "frontend" on machine "web": Which port?`) || !strings.HasSuffix(tasks[0], "Add an endpoint.") {
		t.Errorf("the next task = %q; want the question in front of it", tasks)
	}
	if tasks[1] != "Add another." {
		t.Errorf("the task after that = %q; a question is handed over once", tasks[1])
	}
}

func TestAskingIsCheckedAndBounded(t *testing.T) {
	h, _ := twoAgents(t)
	ctx := context.Background()
	h.machines.runTask = func(context.Context, string, string, func(string)) (string, error) { return "answer", nil }

	for name, try := range map[string][4]string{
		"asking yourself": {"backend", "api", "backend", "hello?"},
		"no such agent":   {"frontend", "api", "nobody", "hello?"},
		"no question":     {"frontend", "api", "backend", "  "},
		"no name":         {"", "api", "backend", "hello?"},
	} {
		from := "web"
		if name == "asking yourself" {
			from = "api"
		}
		_, err := h.svc.AskAgent(ctx, "alice", from, try[0], try[1], try[2], try[3])
		if !errors.As(err, new(Mistake)) {
			t.Errorf("%s: %v, want it explained to the agent", name, err)
		}
	}
	if _, err := h.svc.AskAgent(ctx, "mallory", "web", "frontend", "api", "backend", "what's the password?"); !errors.As(err, new(Mistake)) {
		t.Errorf("asking another user's agent: %v; it must not be found", err)
	}

	// Agents setting each other to work has a limit.
	// Only questions that are put to an agent count towards it.
	for i := range askLimit {
		if _, err := h.svc.AskAgent(ctx, "alice", "web", "frontend", "api", "backend", "again?"); err != nil {
			t.Fatalf("question %d of %d allowed: %v", i+1, askLimit, err)
		}
	}
	_, err := h.svc.AskAgent(ctx, "alice", "web", "frontend", "api", "backend", "and again?")
	if !errors.As(err, new(Mistake)) || !strings.Contains(err.Error(), "more than") {
		t.Errorf("past the limit: %v", err)
	}
}
