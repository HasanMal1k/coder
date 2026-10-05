package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HasanMal1k/coder/internal/guestapi"
	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/store"
)

// fakeAgent stands in for the OpenCode server inside a machine, speaking the
// parts of its API the worker uses.
type fakeAgent struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	sessions map[string][]string // session ID -> prompts received
	aborted  []string
	watchers []chan string
	// hangHealth is how many health checks to leave unanswered.
	hangHealth int
	// work is what the agent does with a prompt: it may emit events, and
	// returns the reply to send.
	work func(ctx context.Context, a *fakeAgent, sessionID, prompt string) (status int, body string)
}

func newFakeAgent(t *testing.T) *fakeAgent {
	t.Helper()
	a := &fakeAgent{t: t, sessions: map[string][]string{}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /global/health", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		hang := a.hangHealth > 0
		a.hangHealth--
		a.mu.Unlock()

		if hang {
			// Accepted and never answered, as OpenCode does with a request
			// that arrives just as it starts.
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `{"healthy":true}`)
	})
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		id := fmt.Sprintf("ses_%d", len(a.sessions)+1)
		a.sessions[id] = nil
		a.mu.Unlock()
		fmt.Fprintf(w, `{"id":%q}`, id)
	})
	mux.HandleFunc("GET /session/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		_, ok := a.sessions[r.PathValue("id")]
		a.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"id":%q}`, r.PathValue("id"))
	})
	mux.HandleFunc("POST /session/{id}/abort", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.aborted = append(a.aborted, r.PathValue("id"))
		a.mu.Unlock()
		fmt.Fprint(w, `true`)
	})
	mux.HandleFunc("GET /event", func(w http.ResponseWriter, r *http.Request) {
		events := make(chan string, 64)
		a.mu.Lock()
		a.watchers = append(a.watchers, events)
		a.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"server.connected\",\"properties\":{}}\n\n")
		w.(http.Flusher).Flush()

		for {
			select {
			case event := <-events:
				fmt.Fprintf(w, "data: %s\n\n", event)
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("POST /session/{id}/message", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"parts"`
		}
		json.NewDecoder(r.Body).Decode(&in)

		id := r.PathValue("id")
		a.mu.Lock()
		a.sessions[id] = append(a.sessions[id], in.Parts[0].Text)
		work := a.work
		a.mu.Unlock()

		status, body := http.StatusOK, reply("Done.")
		if work != nil {
			status, body = work(r.Context(), a, id, in.Parts[0].Text)
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	})

	a.server = httptest.NewServer(mux)
	t.Cleanup(a.server.Close)

	return a
}

// emit sends an event to everyone watching the agent.
func (a *fakeAgent) emit(event string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, w := range a.watchers {
		w <- event
	}
}

func (a *fakeAgent) prompts(sessionID string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.sessions[sessionID]...)
}

func toolEvent(sessionID, callID, tool, status, input string) string {
	return fmt.Sprintf(`{"type":"message.part.updated","properties":{"sessionID":%q,"part":{"sessionID":%q,"type":"tool","tool":%q,"callID":%q,"state":{"status":%q,"input":%s}}}}`,
		sessionID, sessionID, tool, callID, status, input)
}

func reply(text string) string {
	raw, _ := json.Marshal(map[string]any{
		"info": map[string]any{"role": "assistant", "finish": "stop"},
		"parts": []map[string]string{
			{"type": "step-start"},
			{"type": "text", "text": text},
			{"type": "step-finish"},
		},
	})
	return string(raw)
}

// fakeInstances is the instance service, with every machine's agent port
// leading to the same fake agent.
type fakeInstances struct {
	mu        sync.Mutex
	instances []*store.Instance
	agent     *fakeAgent
	// dialErr, when set, is what reaching a machine's agent fails with.
	dialErr   error
	connected map[string]int
	started   []string
	sizes     []instance.Size
	present   int
	// programs is what else listens on the machine: port to the address
	// that stands in for it.
	programs map[int]string
}

func (f *fakeInstances) add(owner, name, status string) *store.Instance {
	f.mu.Lock()
	defer f.mu.Unlock()
	inst := &store.Instance{ID: "id-" + name, OwnerID: owner, Name: name, Status: status}
	f.instances = append(f.instances, inst)
	return inst
}

func (f *fakeInstances) List(_ context.Context, ownerID string) ([]*store.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*store.Instance
	for _, inst := range f.instances {
		if inst.OwnerID == ownerID {
			copied := *inst
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (f *fakeInstances) Create(_ context.Context, ownerID, name string, opts instance.Options) (*store.Instance, error) {
	size := opts.Size
	if name == "Bad Name" {
		return nil, instance.ErrInvalidName
	}
	f.mu.Lock()
	f.sizes = append(f.sizes, size)
	f.mu.Unlock()

	f.mu.Lock()
	for _, inst := range f.instances {
		if inst.OwnerID == ownerID && inst.Name == name {
			f.mu.Unlock()
			return nil, instance.ErrNameTaken
		}
	}
	f.mu.Unlock()

	inst := f.add(ownerID, name, instance.StatusStopped)
	f.mu.Lock()
	inst.CPUs, inst.MemoryMB, inst.DiskGB, inst.Stack = size.CPUs, size.MemoryMB, size.DiskGB, opts.Stack
	copied := *inst
	f.mu.Unlock()
	return &copied, nil
}

func (f *fakeInstances) setStatus(ownerID, id, status string) (*store.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, inst := range f.instances {
		if inst.OwnerID == ownerID && inst.ID == id {
			inst.Status = status
			if status == instance.StatusRunning {
				f.started = append(f.started, inst.Name)
			}
			copied := *inst
			return &copied, nil
		}
	}
	return nil, instance.ErrNotFound
}

func (f *fakeInstances) Start(_ context.Context, ownerID, id string) (*store.Instance, error) {
	return f.setStatus(ownerID, id, instance.StatusRunning)
}

func (f *fakeInstances) Stop(_ context.Context, ownerID, id string) (*store.Instance, error) {
	return f.setStatus(ownerID, id, instance.StatusStopped)
}

func (f *fakeInstances) DialPort(ctx context.Context, id string, port int) (net.Conn, error) {
	f.mu.Lock()
	dialErr := f.dialErr
	f.mu.Unlock()

	// Another program on the machine, if a test has put one there.
	f.mu.Lock()
	other, ok := f.programs[port]
	f.mu.Unlock()
	if ok {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", other)
	}

	if port != guestapi.AgentPort {
		return nil, fmt.Errorf("dialled port %d, want the agent's", port)
	}
	if dialErr != nil {
		return nil, dialErr
	}

	var d net.Dialer
	return d.DialContext(ctx, "tcp", f.agent.server.Listener.Addr().String())
}

func (f *fakeInstances) ForMachine(_ context.Context, id string) (*store.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, inst := range f.instances {
		if inst.ID == id {
			copied := *inst
			return &copied, nil
		}
	}
	return nil, instance.ErrNotFound
}

func (f *fakeInstances) MachineConnected(id string) func() {
	f.mu.Lock()
	f.connected[id]++
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.connected[id]--
		f.mu.Unlock()
	}
}

func (f *fakeInstances) UserConnected(string) func() {
	f.mu.Lock()
	f.present++
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.present--
		f.mu.Unlock()
	}
}

// memSessions remembers agents' sessions in memory.
type memSessions struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSessions) Session(_ context.Context, agentID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.m[agentID]
	if !ok {
		return "", store.ErrNotFound
	}
	return id, nil
}

func (s *memSessions) SetSession(_ context.Context, agentID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[agentID] = sessionID
	return nil
}

// agentRef is an agent for a test task. Tests use the ID to tell
// conversations apart.
func agentRef(id string) AgentRef {
	return AgentRef{ID: id, Name: "coder"}
}

func newFleet(t *testing.T) (*Fleet, *fakeInstances, *fakeAgent) {
	t.Helper()
	agent := newFakeAgent(t)
	instances := &fakeInstances{agent: agent, connected: map[string]int{}}
	fleet := NewFleet(instances, &memSessions{m: map[string]string{}})
	return fleet, instances, agent
}

// notes collects progress lines.
type notes struct {
	mu    sync.Mutex
	lines []string
}

func (n *notes) add(line string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lines = append(n.lines, line)
}

func (n *notes) all() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.lines...)
}

func ignore(string) {}

// ---------- tests

func TestRunTaskSendsTheTaskAndReturnsTheReport(t *testing.T) {
	fleet, instances, agent := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)

	agent.work = func(_ context.Context, a *fakeAgent, session, _ string) (int, string) {
		if !fleet.TaskActive("id-api") {
			t.Error("the machine is not marked as working while its task runs")
		}
		if instances.connected["id-api"] != 1 {
			t.Error("the machine is not marked as in use while its task runs")
		}

		// The same tool call is announced as it progresses; another
		// session's events share the stream.
		a.emit(toolEvent(session, "c1", "write", "pending", `{}`))
		a.emit(toolEvent(session, "c1", "write", "running", `{"filePath":"/home/coder/app/main.py","content":"print(1)"}`))
		a.emit(toolEvent(session, "c1", "write", "completed", `{"filePath":"/home/coder/app/main.py","content":"print(1)"}`))
		a.emit(toolEvent("ses_other", "x1", "bash", "running", `{"command":"rm -rf /"}`))
		a.emit(toolEvent(session, "c2", "bash", "running", `{"command":"cd app &&\n  python3 main.py"}`))
		a.emit(`{"type":"session.idle","properties":{"sessionID":"` + session + `"}}`)
		a.emit(`not json`)
		// Give the events time to arrive before the reply ends the task.
		time.Sleep(100 * time.Millisecond)

		return 200, reply("Built the app in /home/coder/app.")
	}

	var got notes
	report, err := fleet.RunTask(context.Background(), "alice", "api", agentRef("chat-1"), "build an app", got.add)
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	if report != "Built the app in /home/coder/app." {
		t.Errorf("report = %q", report)
	}
	if lines := got.all(); !equal(lines, []string{"Writing app/main.py", "Running cd app && python3 main.py"}) {
		t.Errorf("progress = %q", lines)
	}

	prompts := agent.prompts("ses_1")
	if len(prompts) != 1 || !strings.HasSuffix(prompts[0], "The task:\n\nbuild an app") || !strings.Contains(prompts[0], `You are "coder", a coding agent on the machine "api"`) {
		t.Errorf("the agent was sent %q", prompts)
	}

	if fleet.TaskActive("id-api") || instances.connected["id-api"] != 0 {
		t.Error("the machine is still marked as working after its task ended")
	}
}

func TestTasksForOneAgentShareASessionAndAgentsDoNot(t *testing.T) {
	fleet, instances, agent := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)
	ctx := context.Background()

	for _, step := range []struct{ agent, task string }{
		{"agent-1", "first"}, {"agent-1", "second"}, {"agent-2", "other"},
	} {
		if _, err := fleet.RunTask(ctx, "alice", "api", agentRef(step.agent), step.task, ignore); err != nil {
			t.Fatalf("RunTask(%s): %v", step.task, err)
		}
	}

	if got := len(agent.prompts("ses_1")); got != 2 {
		t.Errorf("the first agent's session received %d tasks, want both of its tasks", got)
	}
	if got := len(agent.prompts("ses_2")); got != 1 {
		t.Errorf("the second agent's session received %d tasks, want 1", got)
	}
}

func TestALostSessionIsReplaced(t *testing.T) {
	fleet, instances, agent := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)
	ctx := context.Background()

	fleet.RunTask(ctx, "alice", "api", agentRef("chat-1"), "first", ignore)

	// The agent forgets the session, as it would if its data were wiped.
	agent.mu.Lock()
	delete(agent.sessions, "ses_1")
	agent.sessions["placeholder"] = nil
	agent.mu.Unlock()

	if _, err := fleet.RunTask(ctx, "alice", "api", agentRef("chat-1"), "second", ignore); err != nil {
		t.Fatalf("RunTask after the session was lost: %v", err)
	}
	if got := agent.prompts("ses_2"); len(got) != 1 {
		t.Errorf("the replacement session received %v", got)
	}
}

func TestAStoppedMachineIsStartedForATask(t *testing.T) {
	fleet, instances, _ := newFleet(t)
	instances.add("alice", "api", instance.StatusStopped)

	var got notes
	if _, err := fleet.RunTask(context.Background(), "alice", "api", agentRef("chat-1"), "go", got.add); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	if !equal(instances.started, []string{"api"}) {
		t.Errorf("started = %v, want the machine started", instances.started)
	}
	if lines := got.all(); len(lines) == 0 || lines[0] != "Starting the machine" {
		t.Errorf("progress = %q, want it to say the machine is being started", lines)
	}
}

func TestAgentsOnOneMachineWorkAtOnce(t *testing.T) {
	fleet, instances, agent := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)

	// Each task refuses to finish until all three have started. Run one
	// after the other, they would wait for ever.
	var started sync.WaitGroup
	started.Add(3)
	allStarted := make(chan struct{})
	go func() { started.Wait(); close(allStarted) }()

	agent.work = func(context.Context, *fakeAgent, string, string) (int, string) {
		started.Done()
		select {
		case <-allStarted:
			return 200, reply("ok")
		case <-time.After(5 * time.Second):
			return 500, "the other agents never started: agents on one machine are not working at once"
		}
	}

	var wg sync.WaitGroup
	for _, name := range []string{"backend", "frontend", "tests"} {
		wg.Go(func() {
			ref := AgentRef{ID: "agent-" + name, Name: name}
			if _, err := fleet.RunTask(context.Background(), "alice", "api", ref, "go", ignore); err != nil {
				t.Errorf("RunTask(%s): %v", name, err)
			}
		})
	}

	<-allStarted
	if !fleet.TaskActive("id-api") {
		t.Error("the machine is not marked as working while its agents work")
	}
	wg.Wait()

	if fleet.TaskActive("id-api") || len(fleet.tasks) != 0 {
		t.Errorf("tasks still counted after all ended: %v", fleet.tasks)
	}
	if got := len(agent.sessions); got != 3 {
		t.Errorf("%d sessions, want one for each agent", got)
	}
	for id, prompts := range agent.sessions {
		if len(prompts) != 1 {
			t.Errorf("session %s received %d tasks, want 1", id, len(prompts))
		}
	}
}

func TestCancellingATaskStopsTheAgent(t *testing.T) {
	fleet, instances, agent := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)

	started := make(chan struct{})
	agent.work = func(ctx context.Context, _ *fakeAgent, _, _ string) (int, string) {
		close(started)
		<-ctx.Done()
		return 200, reply("never seen")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	_, err := fleet.RunTask(ctx, "alice", "api", agentRef("chat-1"), "long", ignore)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !equal(agent.aborted, []string{"ses_1"}) {
		t.Errorf("aborted = %v; the agent must be told to stop, or it carries on working", agent.aborted)
	}
}

func TestATaskThatTakesTooLongIsStopped(t *testing.T) {
	fleet, instances, agent := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)
	fleet.taskTimeout = 100 * time.Millisecond

	agent.work = func(ctx context.Context, _ *fakeAgent, _, _ string) (int, string) {
		<-ctx.Done()
		return 200, reply("never seen")
	}

	_, err := fleet.RunTask(context.Background(), "alice", "api", agentRef("chat-1"), "long", ignore)
	if err == nil || !strings.Contains(err.Error(), "took longer than") {
		t.Fatalf("err = %v, want it to say the task took too long", err)
	}
	if len(agent.aborted) != 1 {
		t.Errorf("aborted = %v, want the agent told to stop", agent.aborted)
	}
}

func TestAgentFailuresAreReported(t *testing.T) {
	fleet, instances, agent := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)

	agent.work = func(context.Context, *fakeAgent, string, string) (int, string) {
		return 200, `{"info":{"error":{"name":"APIError","data":{"message":"model returned 402: insufficient balance"}}},"parts":[]}`
	}
	_, err := fleet.RunTask(context.Background(), "alice", "api", agentRef("chat-1"), "go", ignore)
	if err == nil || !strings.Contains(err.Error(), "insufficient balance") {
		t.Errorf("err = %v, want the agent's own error", err)
	}

	agent.work = func(context.Context, *fakeAgent, string, string) (int, string) {
		return 500, "boom"
	}
	_, err = fleet.RunTask(context.Background(), "alice", "api", agentRef("chat-2"), "go", ignore)
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want the status and body", err)
	}
}

func TestAMachineWithoutAnAgentIsExplained(t *testing.T) {
	for name, dialErr := range map[string]error{
		"old guest daemon":  instance.ErrPortUnsupported,
		"nothing listening": instance.ErrPortClosed,
	} {
		t.Run(name, func(t *testing.T) {
			fleet, instances, _ := newFleet(t)
			instances.add("alice", "old", instance.StatusRunning)
			instances.dialErr = dialErr
			fleet.agentStartTimeout = 50 * time.Millisecond

			_, err := fleet.RunTask(context.Background(), "alice", "old", agentRef("chat-1"), "go", ignore)
			if err == nil || !strings.Contains(err.Error(), "has no coding agent") {
				t.Errorf("err = %v, want it to say the machine has no agent", err)
			}
		})
	}
}

func TestAMachineThatNeverComesOnlineGivesUp(t *testing.T) {
	fleet, instances, _ := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)
	instances.dialErr = instance.ErrGuestOffline
	fleet.readyTimeout = 50 * time.Millisecond

	_, err := fleet.RunTask(context.Background(), "alice", "api", agentRef("chat-1"), "go", ignore)
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Errorf("err = %v, want it to say the machine did not become ready", err)
	}
}

func TestAnAgentThatIsSlowToStartIsWaitedFor(t *testing.T) {
	fleet, instances, _ := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)
	instances.dialErr = instance.ErrGuestOffline

	// Offline, then online with no agent yet, then ready: a machine booting.
	go func() {
		time.Sleep(100 * time.Millisecond)
		instances.mu.Lock()
		instances.dialErr = instance.ErrPortClosed
		instances.mu.Unlock()

		time.Sleep(100 * time.Millisecond)
		instances.mu.Lock()
		instances.dialErr = nil
		instances.mu.Unlock()
	}()

	if _, err := fleet.RunTask(context.Background(), "alice", "api", agentRef("chat-1"), "go", ignore); err != nil {
		t.Fatalf("RunTask: %v", err)
	}
}

func TestAHealthCheckThatGetsNoAnswerIsRetried(t *testing.T) {
	fleet, instances, agent := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)
	agent.hangHealth = 1

	finished := make(chan error, 1)
	go func() {
		_, err := fleet.RunTask(context.Background(), "alice", "api", agentRef("chat-1"), "go", ignore)
		finished <- err
	}()

	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("RunTask: %v", err)
		}
	case <-time.After(healthTimeout + 10*time.Second):
		t.Fatal("RunTask is still waiting on a health check that will never be answered")
	}
}

func TestCreateStartsTheMachineAndWaitsForItsAgent(t *testing.T) {
	fleet, instances, _ := newFleet(t)

	var got notes
	m, err := fleet.Create(context.Background(), "alice", MachineSpec{Name: "api", Size: "small", DiskGB: 50}, got.add)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if m.Name != "api" || m.Status != instance.StatusRunning {
		t.Errorf("machine = %+v, want it running", m)
	}
	if m.CPUs != 1 || m.MemoryMB != 1024 || m.DiskGB != 50 {
		t.Errorf("machine = %+v, want the small size with a 50 GB disk", m)
	}
	if !equal(instances.started, []string{"api"}) {
		t.Errorf("started = %v", instances.started)
	}
	if lines := got.all(); !equal(lines, []string{"Creating the machine", "Starting it", "Waiting for its coding agent"}) {
		t.Errorf("progress = %q", lines)
	}

	// With nothing said about size, the defaults.
	if _, err := fleet.Create(context.Background(), "alice", MachineSpec{Name: "web"}, ignore); err != nil {
		t.Fatalf("Create with defaults: %v", err)
	}
	if want := (instance.Size{CPUs: 2, MemoryMB: 2048, DiskGB: 10}); instances.sizes[1] != want {
		t.Errorf("default size = %+v, want %+v", instances.sizes[1], want)
	}
}

func TestErrorsAreWordedForTheModel(t *testing.T) {
	fleet, instances, _ := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)
	instances.add("bob", "bobs", instance.StatusRunning)
	ctx := context.Background()

	if _, err := fleet.Create(ctx, "alice", MachineSpec{Name: "api"}, ignore); err == nil || !strings.Contains(err.Error(), "already has a machine") {
		t.Errorf("duplicate name: %v", err)
	}
	if _, err := fleet.Create(ctx, "alice", MachineSpec{Name: "Bad Name"}, ignore); err == nil || !strings.Contains(err.Error(), "not a valid machine name") {
		t.Errorf("invalid name: %v", err)
	}
	before := len(instances.sizes)
	for _, spec := range []MachineSpec{{Name: "big", Size: "enormous"}, {Name: "big", DiskGB: 7}} {
		_, err := fleet.Create(ctx, "alice", spec, ignore)
		if err == nil || !strings.Contains(err.Error(), "small, medium, large") || !strings.Contains(err.Error(), "10, 20 or 50") {
			t.Errorf("%+v: %v; want the sizes on offer spelled out", spec, err)
		}
	}
	if len(instances.sizes) != before {
		t.Error("a machine was created in a size that is not on offer")
	}
	if _, err := fleet.Find(ctx, "alice", "ghost"); err == nil || !strings.Contains(err.Error(), "no machine called") {
		t.Errorf("unknown machine: %v", err)
	}
	// Another user's machine is as good as nonexistent.
	if _, err := fleet.RunTask(ctx, "alice", "bobs", agentRef("chat-1"), "go", ignore); err == nil || !strings.Contains(err.Error(), "no machine called") {
		t.Errorf("another user's machine: %v", err)
	}
}

func TestDescribe(t *testing.T) {
	input := func(s string) map[string]json.RawMessage {
		var m map[string]json.RawMessage
		json.Unmarshal([]byte(s), &m)
		return m
	}

	for _, c := range []struct{ tool, input, want string }{
		{"write", `{"filePath":"/home/coder/web/index.html"}`, "Writing web/index.html"},
		{"edit", `{"filePath":"/etc/hosts"}`, "Editing /etc/hosts"},
		{"read", `{}`, "Reading a file"},
		{"bash", `{"command":"npm install\n"}`, "Running npm install"},
		{"grep", `{"pattern":"TODO"}`, "Searching for TODO"},
		{"todowrite", `{"todos":[]}`, "Updating its plan"},
		{"mystery", `{}`, "Using mystery"},
	} {
		if got := describe(c.tool, input(c.input)); got != c.want {
			t.Errorf("describe(%s) = %q, want %q", c.tool, got, c.want)
		}
	}

	long := describe("bash", input(`{"command":"`+strings.Repeat("x", 1000)+`"}`))
	if len([]rune(long)) != maxProgressLength {
		t.Errorf("a long note is %d characters, want it cut to %d", len([]rune(long)), maxProgressLength)
	}
}

func TestReadLineSkipsLinesThatAreTooLong(t *testing.T) {
	fleet, instances, agent := newFleet(t)
	instances.add("alice", "api", instance.StatusRunning)

	agent.work = func(_ context.Context, a *fakeAgent, session, _ string) (int, string) {
		// A file bigger than a line may be: the event is skipped, and the
		// stream carries on.
		huge := `{"filePath":"/home/coder/big.bin","content":"` + strings.Repeat("A", maxEventLine) + `"}`
		a.emit(toolEvent(session, "c1", "write", "running", huge))
		a.emit(toolEvent(session, "c2", "bash", "running", `{"command":"ls"}`))
		time.Sleep(200 * time.Millisecond)
		return 200, reply("ok")
	}

	var got notes
	if _, err := fleet.RunTask(context.Background(), "alice", "api", agentRef("chat-1"), "go", got.add); err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if lines := got.all(); !equal(lines, []string{"Running ls"}) {
		t.Errorf("progress = %q, want the oversized event skipped and the next one reported", lines)
	}
}

func TestGetAsksAProgramOnAMachineForAPage(t *testing.T) {
	fleet, instances, _ := newFleet(t)
	inst := instances.add("alice", "api", "running")

	program := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi.json" || r.Host != "localhost:8000" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"openapi":"3.0.0","paths":{}}`)
	}))
	defer program.Close()
	instances.mu.Lock()
	instances.programs = map[int]string{8000: program.Listener.Addr().String()}
	instances.mu.Unlock()

	status, body, err := fleet.Get(context.Background(), inst.ID, 8000, "/openapi.json")
	if err != nil || status != 200 || string(body) != `{"openapi":"3.0.0","paths":{}}` {
		t.Errorf("Get = %d %q %v", status, body, err)
	}

	status, _, err = fleet.Get(context.Background(), inst.ID, 8000, "/nothing")
	if err != nil || status != 404 {
		t.Errorf("a page that is not there = %d, %v; want its 404, not a failure", status, err)
	}

	// Nothing listening there is a failure, and says so.
	if _, _, err := fleet.Get(context.Background(), inst.ID, 9999, "/"); err == nil {
		t.Error("asking a port nothing listens on succeeded")
	}
}
