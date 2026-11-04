package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HasanMal1k/coder/internal/store"
)

// ---------- fakes

// memStore keeps chats in memory.
type memStore struct {
	mu     sync.Mutex
	chats  map[string]*store.Chat
	events map[string][]*store.ChatEvent
}

func newMemStore() *memStore {
	return &memStore{chats: map[string]*store.Chat{}, events: map[string][]*store.ChatEvent{}}
}

func (m *memStore) Create(_ context.Context, chat *store.Chat) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	chat.ID = uuid.NewString()
	copied := *chat
	m.chats[chat.ID] = &copied
	return nil
}

func (m *memStore) GetByID(_ context.Context, id, ownerID string) (*store.Chat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	chat, ok := m.chats[id]
	if !ok || chat.OwnerID != ownerID {
		return nil, store.ErrNotFound
	}
	copied := *chat
	return &copied, nil
}

func (m *memStore) ListByOwner(_ context.Context, ownerID string) ([]*store.Chat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*store.Chat
	for _, chat := range m.chats {
		if chat.OwnerID == ownerID {
			copied := *chat
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (m *memStore) ListByStatus(_ context.Context, status string) ([]*store.Chat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*store.Chat
	for _, chat := range m.chats {
		if chat.Status == status {
			copied := *chat
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (m *memStore) TransitionStatus(_ context.Context, id string, from []string, to string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	chat, ok := m.chats[id]
	if !ok {
		return false, nil
	}
	for _, f := range from {
		if chat.Status == f {
			chat.Status = to
			return true, nil
		}
	}
	return false, nil
}

func (m *memStore) SetStatus(_ context.Context, id, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if chat, ok := m.chats[id]; ok {
		chat.Status = status
	}
	return nil
}

func (m *memStore) SetTitle(_ context.Context, id, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if chat, ok := m.chats[id]; ok {
		chat.Title = title
	}
	return nil
}

func (m *memStore) Delete(_ context.Context, id, ownerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	chat, ok := m.chats[id]
	if !ok || chat.OwnerID != ownerID {
		return store.ErrNotFound
	}
	delete(m.chats, id)
	delete(m.events, id)
	return nil
}

func (m *memStore) AppendEvent(_ context.Context, chatID, kind string, payload json.RawMessage) (*store.ChatEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	event := &store.ChatEvent{ChatID: chatID, Seq: int64(len(m.events[chatID]) + 1), Kind: kind, Payload: payload}
	m.events[chatID] = append(m.events[chatID], event)
	return event, nil
}

func (m *memStore) ListEvents(_ context.Context, chatID string, after int64) ([]*store.ChatEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*store.ChatEvent
	for _, event := range m.events[chatID] {
		if event.Seq > after {
			out = append(out, event)
		}
	}
	return out, nil
}

// memAgents keeps agents in memory.
type memAgents struct {
	mu       sync.Mutex
	agents   map[string]*store.Agent
	tasks    []*store.AgentTask
	notes    []*store.AgentNote
	messages map[string][]*store.AgentMessage
}

func newMemAgents() *memAgents {
	return &memAgents{agents: map[string]*store.Agent{}, messages: map[string][]*store.AgentMessage{}}
}

func (m *memAgents) Ensure(_ context.Context, ownerID, chatID, instanceID, name, handle string) (*store.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.agents {
		if a.ChatID == chatID && a.InstanceID == instanceID && a.Name == name {
			copied := *a
			return &copied, nil
		}
	}
	for _, a := range m.agents {
		if a.OwnerID == ownerID && a.Handle == handle {
			return nil, store.ErrHandleTaken
		}
	}
	a := &store.Agent{
		ID: uuid.NewString(), OwnerID: ownerID, ChatID: chatID, InstanceID: instanceID,
		// The pretend machines' IDs are "id-" and their name.
		MachineName: strings.TrimPrefix(instanceID, "id-"),
		Name:        name, Handle: handle, Status: AgentIdle, CreatedAt: time.Now(),
	}
	m.agents[a.ID] = a
	copied := *a
	return &copied, nil
}

func (m *memAgents) GetByID(_ context.Context, id, ownerID string) (*store.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok || a.OwnerID != ownerID {
		return nil, store.ErrNotFound
	}
	copied := *a
	return &copied, nil
}

func (m *memAgents) list(keep func(*store.Agent) bool) []*store.Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*store.Agent
	for _, a := range m.agents {
		if keep(a) {
			copied := *a
			out = append(out, &copied)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (m *memAgents) ListByOwner(_ context.Context, ownerID string) ([]*store.Agent, error) {
	return m.list(func(a *store.Agent) bool { return a.OwnerID == ownerID }), nil
}

func (m *memAgents) ListByChat(_ context.Context, chatID string) ([]*store.Agent, error) {
	return m.list(func(a *store.Agent) bool { return a.ChatID == chatID }), nil
}

func (m *memAgents) ListByStatus(_ context.Context, status string) ([]*store.Agent, error) {
	return m.list(func(a *store.Agent) bool { return a.Status == status }), nil
}

func (m *memAgents) Begin(_ context.Context, id, task string, background bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok || a.Status == AgentWorking {
		return false, nil
	}
	now := time.Now()
	a.Status, a.Task, a.Background, a.Report, a.Activity, a.StartedAt = AgentWorking, task, background, "", "", &now
	m.tasks = append(m.tasks, &store.AgentTask{AgentID: id, Task: task, Background: background, Status: "working", StartedAt: now})
	return true, nil
}

// current returns the task the agent is working on, or nil. The caller holds
// the lock.
func (m *memAgents) current(id string) *store.AgentTask {
	for i := len(m.tasks) - 1; i >= 0; i-- {
		if m.tasks[i].AgentID == id && m.tasks[i].Status == "working" {
			return m.tasks[i]
		}
	}
	return nil
}

func (m *memAgents) ListTasks(_ context.Context, ownerID string, perAgent int) ([]*store.AgentTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := map[string]int{}
	var out []*store.AgentTask
	for i := len(m.tasks) - 1; i >= 0; i-- {
		t := m.tasks[i]
		if a, ok := m.agents[t.AgentID]; !ok || a.OwnerID != ownerID || kept[t.AgentID] >= perAgent {
			continue
		}
		kept[t.AgentID]++
		copied := *t
		out = append([]*store.AgentTask{&copied}, out...)
	}
	return out, nil
}

func (m *memAgents) AddNote(_ context.Context, note *store.AgentNote) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	note.CreatedAt = time.Now()
	copied := *note
	m.notes = append(m.notes, &copied)
	return nil
}

func (m *memAgents) ListNotes(_ context.Context, ownerID string, limit int) ([]*store.AgentNote, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*store.AgentNote
	for _, n := range m.notes {
		if n.OwnerID == ownerID {
			copied := *n
			out = append(out, &copied)
		}
	}
	return out[max(0, len(out)-limit):], nil
}

func (m *memAgents) Finish(_ context.Context, id, status, report string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.agents[id]; ok {
		now := time.Now()
		a.Status, a.Report, a.Activity, a.FinishedAt = status, report, "", &now
		if t := m.current(id); t != nil {
			t.Status, t.Report, t.FinishedAt = status, report, &now
			if status == AgentIdle {
				t.Status = "done"
			}
		}
	}
	return nil
}

func (m *memAgents) SetActivity(_ context.Context, id, activity string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.agents[id]; ok {
		a.Activity = activity
		if t := m.current(id); t != nil {
			t.Log = append(t.Log, activity)
		}
	}
	return nil
}

// byName returns the chat's agent with the given name, or nil.
func (m *memAgents) byName(chatID, name string) *store.Agent {
	for _, a := range m.list(func(a *store.Agent) bool { return a.ChatID == chatID && a.Name == name }) {
		return a
	}
	return nil
}

func (m *memAgents) AddMessage(_ context.Context, toAgentID string, msg *store.AgentMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := *msg
	m.messages[toAgentID] = append(m.messages[toAgentID], &copied)
	return nil
}

func (m *memAgents) TakeMessages(_ context.Context, agentID string, fromOwner bool) ([]*store.AgentMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var taken, left []*store.AgentMessage
	for _, msg := range m.messages[agentID] {
		if msg.FromOwner == fromOwner {
			taken = append(taken, msg)
		} else {
			left = append(left, msg)
		}
	}
	m.messages[agentID] = left
	return taken, nil
}

func (m *memAgents) Handles(_ context.Context, ownerID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var handles []string
	for _, a := range m.agents {
		if a.OwnerID == ownerID {
			handles = append(handles, a.Handle)
		}
	}
	return handles, nil
}

// memAttachments keeps files in memory.
type memAttachments struct {
	mu    sync.Mutex
	files map[string]*store.Attachment
}

func newMemAttachments() *memAttachments {
	return &memAttachments{files: map[string]*store.Attachment{}}
}

func (m *memAttachments) Create(_ context.Context, a *store.Attachment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.ID = uuid.NewString()
	a.Size = len(a.Data)
	a.CreatedAt = time.Now()
	copied := *a
	m.files[a.ID] = &copied
	return nil
}

func (m *memAttachments) Get(_ context.Context, id, chatID, ownerID string) (*store.Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.files[id]
	if !ok || a.ChatID != chatID || a.OwnerID != ownerID {
		return nil, store.ErrNotFound
	}
	copied := *a
	return &copied, nil
}

// memDesigns keeps designs in memory.
type memDesigns struct {
	mu      sync.Mutex
	designs map[string]*store.Architecture
}

func newMemDesigns() *memDesigns {
	return &memDesigns{designs: map[string]*store.Architecture{}}
}

func (m *memDesigns) Save(_ context.Context, a *store.Architecture) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.UpdatedAt = time.Now()
	copied := *a
	m.designs[a.ChatID] = &copied
	return nil
}

func (m *memDesigns) Get(_ context.Context, chatID string) (*store.Architecture, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.designs[chatID]
	if !ok {
		return nil, store.ErrNotFound
	}
	copied := *a
	return &copied, nil
}

func (m *memDesigns) ListApproved(_ context.Context, ownerID string) ([]*store.Architecture, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*store.Architecture
	for _, a := range m.designs {
		if a.OwnerID == ownerID && a.Status == DesignApproved {
			copied := *a
			out = append(out, &copied)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChatID < out[j].ChatID })
	return out, nil
}

// memServices keeps registered services in memory.
type memServices struct {
	mu       sync.Mutex
	services []*store.MachineService
	// owners says who owns each machine, for ListByOwner.
	owners map[string]string
	// revision is the last one given out.
	revision int64
}

func newMemServices() *memServices {
	return &memServices{owners: map[string]string{}}
}

func (m *memServices) Save(_ context.Context, svc *store.MachineService) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revision++
	svc.Revision = m.revision
	copied := *svc
	for i, existing := range m.services {
		if existing.InstanceID == svc.InstanceID && existing.Name == svc.Name {
			m.services[i] = &copied
			return nil
		}
	}
	m.services = append(m.services, &copied)
	return nil
}

func (m *memServices) Delete(_ context.Context, instanceID, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, existing := range m.services {
		if existing.InstanceID == instanceID && existing.Name == name {
			m.services = append(m.services[:i], m.services[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (m *memServices) ListByInstance(_ context.Context, instanceID string) ([]*store.MachineService, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*store.MachineService
	for _, svc := range m.services {
		if svc.InstanceID == instanceID {
			copied := *svc
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (m *memServices) ListByOwner(_ context.Context, ownerID string) ([]*store.MachineService, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*store.MachineService
	for _, svc := range m.services {
		if m.owners[svc.InstanceID] == ownerID {
			copied := *svc
			out = append(out, &copied)
		}
	}
	return out, nil
}

// scriptedLLM answers from a function, and remembers what it was sent.
type scriptedLLM struct {
	mu    sync.Mutex
	calls [][]Message
	reply func(call int, messages []Message) (Message, error)
	// writing, when set, plays the model writing its reply piece by piece.
	writing func(onText func(string))
}

func (l *scriptedLLM) Complete(ctx context.Context, messages []Message, _ []ToolDef, onText func(string)) (Message, error) {
	l.mu.Lock()
	call := len(l.calls)
	l.calls = append(l.calls, messages)
	writing := l.writing
	l.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return Message{}, err
	}
	if writing != nil {
		writing(onText)
	}
	return l.reply(call, messages)
}

func (l *scriptedLLM) sent(call int) []Message {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls[call]
}

func says(text string) Message { return Message{Role: "assistant", Content: text} }

func wants(calls ...ToolCall) Message { return Message{Role: "assistant", ToolCalls: calls} }

func callTool(id, name string, args any) ToolCall {
	raw, _ := json.Marshal(args)
	return ToolCall{ID: id, Type: "function", Function: FunctionCall{Name: name, Arguments: string(raw)}}
}

// fakeMachines is a user's machines, held in memory.
type fakeMachines struct {
	mu       sync.Mutex
	byOwner  map[string][]Machine
	tasks    []string
	specs    []MachineSpec
	runTask  func(ctx context.Context, name, task string, progress func(string)) (string, error)
	failNext error
	// awake counts runs currently keeping the user's machines up.
	awake int
	// open is the ports that answer, as "machine id:port".
	open map[string]bool
	// pages is what those ports serve, as "machine id:port/path".
	pages map[string]string
	// files is what is on machines, as "machine id:path".
	files map[string][]byte
}

func (f *fakeMachines) GetFile(_ context.Context, instanceID, path string, maxSize int) ([]byte, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[instanceID+":"+path]
	if !ok {
		return nil, "", fmt.Errorf("there is no file at %s", path)
	}
	if len(data) > maxSize {
		return nil, "", fmt.Errorf("%s is too large", path)
	}
	return data, path, nil
}

func newFakeMachines() *fakeMachines {
	return &fakeMachines{byOwner: map[string][]Machine{}, open: map[string]bool{}, pages: map[string]string{}}
}

func (f *fakeMachines) List(_ context.Context, ownerID string) ([]Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Machine(nil), f.byOwner[ownerID]...), nil
}

func (f *fakeMachines) Create(_ context.Context, ownerID string, spec MachineSpec, progress func(string)) (Machine, error) {
	f.mu.Lock()
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		f.mu.Unlock()
		return Machine{}, err
	}
	f.specs = append(f.specs, spec)
	f.mu.Unlock()

	progress("Creating the machine")

	f.mu.Lock()
	defer f.mu.Unlock()
	m := Machine{ID: "id-" + spec.Name, Name: spec.Name, Status: "running", CPUs: 4, MemoryMB: 4096, DiskGB: 20}
	f.byOwner[ownerID] = append(f.byOwner[ownerID], m)
	return m, nil
}

func (f *fakeMachines) Find(_ context.Context, ownerID, name string) (Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.byOwner[ownerID] {
		if m.Name == name {
			return m, nil
		}
	}
	return Machine{}, fmt.Errorf("there is no machine called %q", name)
}

func (f *fakeMachines) Start(ctx context.Context, ownerID, name string) (Machine, error) {
	return f.Find(ctx, ownerID, name)
}

func (f *fakeMachines) Stop(ctx context.Context, ownerID, name string) (Machine, error) {
	return f.Find(ctx, ownerID, name)
}

func (f *fakeMachines) RunTask(ctx context.Context, ownerID, name string, agent AgentRef, task string, files []TaskFile, progress func(string)) (string, error) {
	f.mu.Lock()
	f.tasks = append(f.tasks, name+"|"+agent.Name+"|"+task)
	for _, file := range files {
		if f.files == nil {
			f.files = map[string][]byte{}
		}
		f.files["id-"+name+":"+file.Path] = file.Data
	}
	run := f.runTask
	f.mu.Unlock()

	if run != nil {
		return run(ctx, name, task, progress)
	}
	progress("write main.py")
	return "Done on " + name + ".", nil
}

func (f *fakeMachines) Lookup(_ context.Context, instanceID string) (string, Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for owner, machines := range f.byOwner {
		for _, m := range machines {
			if m.ID == instanceID {
				return owner, m, nil
			}
		}
	}
	return "", Machine{}, errors.New("no such machine")
}

func (f *fakeMachines) PortOpen(_ context.Context, instanceID string, port int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.open[fmt.Sprintf("%s:%d", instanceID, port)]
}

func (f *fakeMachines) Get(_ context.Context, instanceID string, port int, path string) (int, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.open[fmt.Sprintf("%s:%d", instanceID, port)] {
		return 0, nil, errors.New("nothing is listening on that port")
	}
	page, ok := f.pages[fmt.Sprintf("%s:%d%s", instanceID, port, path)]
	if !ok {
		return 404, []byte("not found"), nil
	}
	return 200, []byte(page), nil
}

// serve makes a port on a machine answer a path with a page.
func (f *fakeMachines) serve(machine string, port int, path, page string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages[fmt.Sprintf("id-%s:%d%s", machine, port, path)] = page
}

// listen marks a port on a machine as answering, or not.
func (f *fakeMachines) listen(machine string, port int, open bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open[fmt.Sprintf("id-%s:%d", machine, port)] = open
}

func (f *fakeMachines) KeepAwake(ownerID string) func() {
	f.mu.Lock()
	f.awake++
	f.mu.Unlock()

	return func() {
		f.mu.Lock()
		f.awake--
		f.mu.Unlock()
	}
}

// ---------- helpers

type harness struct {
	t        *testing.T
	store    *memStore
	agents   *memAgents
	files    *memAttachments
	designs  *memDesigns
	services *memServices
	llm      *scriptedLLM
	machines *fakeMachines
	svc      *Service
}

func newHarness(t *testing.T, reply func(call int, messages []Message) (Message, error)) *harness {
	t.Helper()
	h := &harness{
		t: t, store: newMemStore(), agents: newMemAgents(), files: newMemAttachments(), designs: newMemDesigns(), services: newMemServices(),
		llm: &scriptedLLM{reply: reply}, machines: newFakeMachines(),
	}
	h.svc = NewService(h.stores(), h.llm, h.machines)
	return h
}

func (h *harness) stores() Stores {
	return Stores{Chats: h.store, Agents: h.agents, Attachments: h.files, Designs: h.designs, Services: h.services}
}

func (h *harness) chat(owner string) string {
	h.t.Helper()
	chat, err := h.svc.CreateChat(context.Background(), owner)
	if err != nil {
		h.t.Fatalf("CreateChat: %v", err)
	}
	return chat.ID
}

func (h *harness) send(owner, chatID, text string) {
	h.t.Helper()
	if err := h.svc.Send(context.Background(), owner, chatID, text); err != nil {
		h.t.Fatalf("Send(%q): %v", text, err)
	}
}

// settle waits for the chat to stop running and returns its status.
func (h *harness) settle(owner, chatID string) string {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		chat, err := h.svc.GetChat(context.Background(), owner, chatID)
		if err != nil {
			h.t.Fatalf("GetChat: %v", err)
		}
		if chat.Status != StatusRunning {
			return chat.Status
		}
		if time.Now().After(deadline) {
			h.t.Fatal("the run never finished")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// kinds lists a chat's events as "kind" or "kind:detail", for comparing.
func (h *harness) kinds(owner, chatID string) []string {
	h.t.Helper()
	events, err := h.svc.Events(context.Background(), owner, chatID, 0)
	if err != nil {
		h.t.Fatalf("Events: %v", err)
	}
	var out []string
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

func (h *harness) payloads(owner, chatID, kind string) []json.RawMessage {
	h.t.Helper()
	events, _ := h.svc.Events(context.Background(), owner, chatID, 0)
	var out []json.RawMessage
	for _, e := range events {
		if e.Kind == kind {
			out = append(out, e.Payload)
		}
	}
	return out
}

func equal(a, b []string) bool {
	return strings.Join(a, ",") == strings.Join(b, ",")
}

// ---------- tests

func TestPlainAnswer(t *testing.T) {
	h := newHarness(t, func(int, []Message) (Message, error) { return says("Hello."), nil })
	id := h.chat("alice")

	h.send("alice", id, "  hi there, please build me something nice  \nsecond line")

	if status := h.settle("alice", id); status != StatusIdle {
		t.Errorf("status = %q, want idle", status)
	}
	if got := h.kinds("alice", id); !equal(got, []string{KindUser, KindAssistant}) {
		t.Errorf("events = %v", got)
	}

	sent := h.llm.sent(0)
	if sent[0].Role != "system" || sent[1].Role != "user" || sent[1].Content != "hi there, please build me something nice  \nsecond line" {
		t.Errorf("model was sent %+v", sent)
	}

	chat, _ := h.svc.GetChat(context.Background(), "alice", id)
	if chat.Title != "hi there, please build me something nice" {
		t.Errorf("title = %q, want the first line of the first message", chat.Title)
	}
}

func TestToolsRunAndTheirResultsGoBackToTheModel(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		switch call {
		case 0:
			return wants(callTool("c1", toolCreateMachine, map[string]any{"name": "api", "size": "large", "disk_gb": 20})), nil
		case 1:
			return wants(callTool("c2", toolCodeOnMachine, map[string]string{"machine": "api", "task": "build it"})), nil
		default:
			return says("All done."), nil
		}
	})
	id := h.chat("alice")

	h.send("alice", id, "make an api")
	h.settle("alice", id)

	want := []string{KindUser, KindAssistant, KindActivity, KindTool, KindAssistant, KindActivity, KindTool, KindAssistant}
	if got := h.kinds("alice", id); !equal(got, want) {
		t.Fatalf("events = %v\n   want   %v", got, want)
	}

	if len(h.machines.tasks) != 1 || h.machines.tasks[0] != "api|coder|build it" {
		t.Errorf("tasks run = %v; want one, on api, for the default agent", h.machines.tasks)
	}

	// The last call to the model must contain each request followed by its
	// result.
	last := h.llm.sent(2)
	var shape []string
	for _, m := range last {
		shape = append(shape, m.Role)
	}
	if !equal(shape, []string{"system", "user", "assistant", "tool", "assistant", "tool"}) {
		t.Errorf("conversation sent = %v", shape)
	}
	if last[3].ToolCallID != "c1" || !strings.Contains(last[3].Content, `Machine "api"`) || !strings.Contains(last[3].Content, "is running and ready") {
		t.Errorf("create result = %+v", last[3])
	}
	if last[5].ToolCallID != "c2" || last[5].Content != "Done on api." {
		t.Errorf("task result = %+v", last[5])
	}

	if len(h.machines.specs) != 1 || h.machines.specs[0] != (MachineSpec{Name: "api", Size: "large", DiskGB: 20}) {
		t.Errorf("machines asked for = %+v; want api, large, with a 20 GB disk", h.machines.specs)
	}
	if !strings.Contains(last[3].Content, "4 vCPU, 4 GB memory, 20 GB disk") {
		t.Errorf("create result = %q; the model should be told what it got", last[3].Content)
	}

	var creating, coding ActivityPayload
	json.Unmarshal(h.payloads("alice", id, KindActivity)[0], &creating)
	json.Unmarshal(h.payloads("alice", id, KindActivity)[1], &coding)
	if creating != (ActivityPayload{ToolCallID: "c1", Machine: "api", Text: "Creating the machine"}) {
		t.Errorf("activity while creating = %+v", creating)
	}
	if coding != (ActivityPayload{ToolCallID: "c2", Machine: "api", Text: "write main.py"}) {
		t.Errorf("activity while coding = %+v", coding)
	}

	var created ToolPayload
	json.Unmarshal(h.payloads("alice", id, KindTool)[0], &created)
	if created.Machine == nil || created.Machine.ID != "id-api" {
		t.Errorf("create result carries machine %+v, want the new machine for the interface to link to", created.Machine)
	}
}

func TestTheModelsReasoningGoesBackToIt(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			reply := wants(callTool("c1", toolListMachines, map[string]any{}))
			reply.ReasoningContent = "They have no machines yet, so I should look first."
			return reply, nil
		}
		return says("You have none."), nil
	})
	id := h.chat("alice")

	h.send("alice", id, "what machines do I have?")
	h.settle("alice", id)

	// DeepSeek requires what it reasoned before a tool call on every later
	// request; without it the model loses the thread between tool calls.
	next := h.llm.sent(1)
	if len(next) < 3 || next[2].Role != "assistant" || next[2].ReasoningContent != "They have no machines yet, so I should look first." {
		t.Errorf("the second request was %+v; want the first reply's reasoning in it", next)
	}

	var stored AssistantPayload
	json.Unmarshal(h.payloads("alice", id, KindAssistant)[0], &stored)
	if stored.Reasoning == "" || stored.Text != "" {
		t.Errorf("stored = %+v; the reasoning is kept with the message, apart from its text", stored)
	}
}

func TestToolsRequestedTogetherRunAtTheSameTime(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			return wants(
				callTool("a", toolCodeOnMachine, map[string]string{"machine": "api", "task": "backend"}),
				callTool("b", toolCodeOnMachine, map[string]string{"machine": "web", "task": "frontend"}),
			), nil
		}
		return says("Both done."), nil
	})
	h.machines.Create(context.Background(), "alice", MachineSpec{Name: "api"}, func(string) {})
	h.machines.Create(context.Background(), "alice", MachineSpec{Name: "web"}, func(string) {})

	// Each task refuses to finish until the other has started. Run one after
	// the other, they would wait for ever.
	var started sync.WaitGroup
	started.Add(2)
	bothStarted := make(chan struct{})
	go func() { started.Wait(); close(bothStarted) }()

	h.machines.runTask = func(ctx context.Context, name, _ string, _ func(string)) (string, error) {
		started.Done()
		select {
		case <-bothStarted:
			return "done " + name, nil
		case <-time.After(5 * time.Second):
			return "", errors.New("the other task never started: tasks are not running concurrently")
		}
	}

	id := h.chat("alice")
	h.send("alice", id, "backend and frontend")
	h.settle("alice", id)

	for _, raw := range h.payloads("alice", id, KindTool) {
		var p ToolPayload
		json.Unmarshal(raw, &p)
		if p.Failed {
			t.Errorf("tool %s failed: %s", p.ToolCallID, p.Content)
		}
	}
}

func TestAskingTheUserPausesUntilTheyAnswer(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			return Message{
				Role:      "assistant",
				Content:   "One thing first.",
				ToolCalls: []ToolCall{callTool("q1", toolAskUser, map[string]any{"question": "Which language?", "options": []string{"Python", "Go"}})},
			}, nil
		}
		return says("Go it is."), nil
	})
	id := h.chat("alice")

	h.send("alice", id, "build a backend")
	if status := h.settle("alice", id); status != StatusWaiting {
		t.Fatalf("status = %q, want waiting", status)
	}
	if got := h.kinds("alice", id); !equal(got, []string{KindUser, KindAssistant}) {
		t.Fatalf("events while waiting = %v; the question must have no result yet", got)
	}
	if len(h.llm.calls) != 1 {
		t.Fatalf("model was called %d times while waiting, want 1", len(h.llm.calls))
	}

	h.send("alice", id, "Go")
	if status := h.settle("alice", id); status != StatusIdle {
		t.Errorf("status = %q, want idle", status)
	}
	if got := h.kinds("alice", id); !equal(got, []string{KindUser, KindAssistant, KindTool, KindAssistant}) {
		t.Errorf("events = %v; the answer must be recorded as the question's result, not as a new message", got)
	}

	resumed := h.llm.sent(1)
	answer := resumed[len(resumed)-1]
	if answer.Role != "tool" || answer.ToolCallID != "q1" || answer.Content != "Go" {
		t.Errorf("model was resumed with %+v, want the answer as the result of q1", answer)
	}
}

func TestSecondMessageWhileWorkingIsRefused(t *testing.T) {
	release := make(chan struct{})
	h := newHarness(t, func(int, []Message) (Message, error) {
		<-release
		return says("ok"), nil
	})
	id := h.chat("alice")

	h.send("alice", id, "first")
	err := h.svc.Send(context.Background(), "alice", id, "second")
	close(release)

	if !errors.Is(err, ErrBusy) {
		t.Errorf("err = %v, want ErrBusy", err)
	}
	h.settle("alice", id)
	if got := h.kinds("alice", id); !equal(got, []string{KindUser, KindAssistant}) {
		t.Errorf("events = %v; the refused message must not be recorded", got)
	}
}

func TestCancelStopsARunInProgress(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		return wants(callTool("c1", toolCodeOnMachine, map[string]string{"machine": "api", "task": "long job"})), nil
	})
	h.machines.Create(context.Background(), "alice", MachineSpec{Name: "api"}, func(string) {})

	running := make(chan struct{})
	h.machines.runTask = func(ctx context.Context, _, _ string, _ func(string)) (string, error) {
		close(running)
		<-ctx.Done()
		return "", ctx.Err()
	}

	id := h.chat("alice")
	h.send("alice", id, "go")
	<-running

	if err := h.svc.Cancel(context.Background(), "alice", id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if status := h.settle("alice", id); status != StatusIdle {
		t.Errorf("status = %q, want idle", status)
	}

	// The interrupted request still gets a result, so the history stays
	// well-formed, and the run is marked as stopped rather than failed.
	if got := h.kinds("alice", id); !equal(got, []string{KindUser, KindAssistant, KindTool, KindError}) {
		t.Fatalf("events = %v", got)
	}
	var stopped ErrorPayload
	json.Unmarshal(h.payloads("alice", id, KindError)[0], &stopped)
	if !stopped.Stopped {
		t.Errorf("error event = %+v, want it marked as stopped by the user", stopped)
	}
}

func TestCancelWithdrawsAQuestion(t *testing.T) {
	h := newHarness(t, func(int, []Message) (Message, error) {
		return wants(callTool("q1", toolAskUser, map[string]string{"question": "Which?"})), nil
	})
	id := h.chat("alice")
	h.send("alice", id, "build")
	h.settle("alice", id)

	if err := h.svc.Cancel(context.Background(), "alice", id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	chat, _ := h.svc.GetChat(context.Background(), "alice", id)
	if chat.Status != StatusIdle {
		t.Errorf("status = %q, want idle", chat.Status)
	}
}

func TestARunThatNeverFinishesIsStopped(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		return wants(callTool(fmt.Sprintf("c%d", call), toolListMachines, map[string]string{})), nil
	})
	id := h.chat("alice")

	h.send("alice", id, "loop")
	if status := h.settle("alice", id); status != StatusIdle {
		t.Errorf("status = %q, want idle", status)
	}
	if len(h.llm.calls) != maxSteps {
		t.Errorf("model was called %d times, want the limit of %d", len(h.llm.calls), maxSteps)
	}
	errs := h.payloads("alice", id, KindError)
	if len(errs) != 1 || !strings.Contains(string(errs[0]), "without finishing") {
		t.Errorf("error events = %s", errs)
	}
}

func TestModelFailureIsReported(t *testing.T) {
	h := newHarness(t, func(int, []Message) (Message, error) {
		return Message{}, errors.New("model returned 401 Unauthorized: bad key")
	})
	id := h.chat("alice")

	h.send("alice", id, "hi")
	if status := h.settle("alice", id); status != StatusIdle {
		t.Errorf("status = %q, want idle so the user can try again", status)
	}
	errs := h.payloads("alice", id, KindError)
	if len(errs) != 1 || !strings.Contains(string(errs[0]), "401 Unauthorized") {
		t.Errorf("error events = %s", errs)
	}
}

func TestToolFailureIsToldToTheModelNotTheUser(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			return wants(callTool("c1", toolCreateMachine, map[string]string{"name": "api"})), nil
		}
		return says("That name is taken; I'll use another."), nil
	})
	h.machines.failNext = errors.New("you already have a machine called \"api\"")
	id := h.chat("alice")

	h.send("alice", id, "make api")
	h.settle("alice", id)

	if got := h.kinds("alice", id); !equal(got, []string{KindUser, KindAssistant, KindTool, KindAssistant}) {
		t.Fatalf("events = %v; a failed tool is a result for the model to act on, not an error that ends the run", got)
	}
	var result ToolPayload
	json.Unmarshal(h.payloads("alice", id, KindTool)[0], &result)
	if !result.Failed || !strings.HasPrefix(result.Content, "Error: you already have") {
		t.Errorf("result = %+v", result)
	}
}

func TestBadToolRequestsAreAnsweredNotFatal(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			return wants(
				ToolCall{ID: "c1", Function: FunctionCall{Name: "launch_rocket", Arguments: "{}"}},
				ToolCall{ID: "c2", Function: FunctionCall{Name: toolCreateMachine, Arguments: "{not json"}},
				callTool("c3", toolShowPreview, map[string]any{"machine": "ghost", "port": 3000}),
				callTool("c4", toolCodeOnMachine, map[string]string{"machine": "ghost", "task": " "}),
			), nil
		}
		return says("ok"), nil
	})
	id := h.chat("alice")

	h.send("alice", id, "go")
	h.settle("alice", id)

	results := map[string]ToolPayload{}
	for _, raw := range h.payloads("alice", id, KindTool) {
		var p ToolPayload
		json.Unmarshal(raw, &p)
		results[p.ToolCallID] = p
	}
	for id, want := range map[string]string{
		"c1": "no tool called",
		"c2": "not valid JSON",
		"c3": "no machine called",
		"c4": "task is empty",
	} {
		if got := results[id]; !got.Failed || !strings.Contains(got.Content, want) {
			t.Errorf("%s = %+v, want a failure mentioning %q", id, got, want)
		}
	}
}

func TestChatsBelongToTheirOwner(t *testing.T) {
	h := newHarness(t, func(int, []Message) (Message, error) { return says("hi"), nil })
	id := h.chat("alice")
	ctx := context.Background()

	if _, err := h.svc.GetChat(ctx, "alice", "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetChat with a malformed ID: %v, want ErrNotFound", err)
	}
	if _, err := h.svc.GetChat(ctx, "mallory", id); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetChat as another user: %v, want ErrNotFound", err)
	}
	if _, err := h.svc.Events(ctx, "mallory", id, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("Events as another user: %v, want ErrNotFound", err)
	}
	if err := h.svc.Send(ctx, "mallory", id, "hello"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Send as another user: %v, want ErrNotFound", err)
	}
	if err := h.svc.Cancel(ctx, "mallory", id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Cancel as another user: %v, want ErrNotFound", err)
	}
	if err := h.svc.DeleteChat(ctx, "mallory", id); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteChat as another user: %v, want ErrNotFound", err)
	}
	if len(h.llm.calls) != 0 {
		t.Error("the model was called on behalf of someone who doesn't own the chat")
	}
}

func TestInvalidAndUnconfigured(t *testing.T) {
	h := newHarness(t, func(int, []Message) (Message, error) { return says("hi"), nil })
	id := h.chat("alice")
	ctx := context.Background()

	for _, text := range []string{"", "   \n ", strings.Repeat("x", maxMessageLength+1)} {
		if err := h.svc.Send(ctx, "alice", id, text); !errors.Is(err, ErrInvalidMessage) {
			t.Errorf("Send(%d chars): %v, want ErrInvalidMessage", len(text), err)
		}
	}

	off := NewService(h.stores(), nil, h.machines)
	if off.Enabled() {
		t.Error("Enabled() with no model")
	}
	if err := off.Send(ctx, "alice", id, "hi"); !errors.Is(err, ErrDisabled) {
		t.Errorf("Send with no model: %v, want ErrDisabled", err)
	}
}

func TestRecoverInterrupted(t *testing.T) {
	h := newHarness(t, func(int, []Message) (Message, error) { return says("hi"), nil })
	stuck, fine := h.chat("alice"), h.chat("alice")
	h.store.SetStatus(context.Background(), stuck, StatusRunning)

	n, err := h.svc.RecoverInterrupted(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("RecoverInterrupted = %d, %v; want 1, nil", n, err)
	}

	chat, _ := h.svc.GetChat(context.Background(), "alice", stuck)
	if chat.Status != StatusIdle {
		t.Errorf("status = %q, want idle", chat.Status)
	}
	if got := h.kinds("alice", stuck); !equal(got, []string{KindError}) {
		t.Errorf("events = %v, want a note explaining what happened", got)
	}
	if got := h.kinds("alice", fine); len(got) != 0 {
		t.Errorf("an idle chat was touched: %v", got)
	}
}

func TestWatchersAreToldOfChanges(t *testing.T) {
	h := newHarness(t, func(int, []Message) (Message, error) { return says("hi"), nil })
	id := h.chat("alice")

	changed, stop := h.svc.Watch(id)
	defer stop()

	h.send("alice", id, "hello")

	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("a watcher was not told about new events")
	}
	h.settle("alice", id)
}

func TestTheReplyCanBeReadWhileItIsWritten(t *testing.T) {
	written := make(chan struct{})
	finish := make(chan struct{})

	h := newHarness(t, func(int, []Message) (Message, error) { return says("Hello there."), nil })
	h.llm.writing = func(onText func(string)) {
		onText("Hello ")
		onText("there")
		close(written)
		<-finish
	}
	id := h.chat("alice")

	changed, stop := h.svc.Watch(id)
	defer stop()

	h.send("alice", id, "hi")
	<-written

	if got := h.svc.Draft(id); got != "Hello there" {
		t.Errorf("draft while writing = %q, want what has been written so far", got)
	}
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Error("watchers were not told the draft was growing")
	}

	close(finish)
	h.settle("alice", id)

	if got := h.svc.Draft(id); got != "" {
		t.Errorf("draft after the reply was recorded = %q, want none", got)
	}
	if got := h.kinds("alice", id); !equal(got, []string{KindUser, KindAssistant}) {
		t.Errorf("events = %v; a draft is never stored", got)
	}
}

func TestAnAbandonedDraftIsCleared(t *testing.T) {
	h := newHarness(t, func(int, []Message) (Message, error) { return Message{}, errors.New("connection reset") })
	h.llm.writing = func(onText func(string)) { onText("Half a sent") }
	id := h.chat("alice")

	h.send("alice", id, "hi")
	h.settle("alice", id)

	if got := h.svc.Draft(id); got != "" {
		t.Errorf("draft after the model failed = %q, want none", got)
	}
}

func TestRepairMakesAnInterruptedHistoryValid(t *testing.T) {
	got := repair([]Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "u"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "a"}, {ID: "b"}}},
		// b's result arrived, a's never did.
		{Role: "tool", ToolCallID: "b", Content: "result b"},
		{Role: "user", Content: "still there?"},
	})

	var shape []string
	for _, m := range got {
		shape = append(shape, m.Role+":"+m.ToolCallID)
	}
	if !equal(shape, []string{"system:", "user:", "assistant:", "tool:a", "tool:b", "user:"}) {
		t.Fatalf("shape = %v; each request must be followed by a result for every call, in order", shape)
	}
	if !strings.Contains(got[3].Content, "interrupted") || got[4].Content != "result b" {
		t.Errorf("results = %q, %q", got[3].Content, got[4].Content)
	}
}

func TestNewsTheModelHadNotSeenComesAfterWhatItWrote(t *testing.T) {
	event := func(seq int64, kind string, payload any) *store.ChatEvent {
		raw, _ := json.Marshal(payload)
		return &store.ChatEvent{Seq: seq, Kind: kind, Payload: raw}
	}
	news := AgentPayload{Agent: "backend", Machine: Machine{Name: "api"}, Content: "done"}

	got := history([]*store.ChatEvent{
		event(1, KindUser, UserPayload{Text: "go"}),
		event(2, KindAssistant, AssistantPayload{ToolCalls: []ToolCall{{ID: "c1"}}, Seen: 1}),
		event(3, KindTool, ToolPayload{ToolCallID: "c1", Content: "working in the background"}),
		// Arrived while the model was writing the next message, which was
		// based on events up to 3.
		event(4, KindAgent, news),
		event(5, KindAssistant, AssistantPayload{Text: "Started.", Seen: 3}),
		// Arrived after the model had seen it: stays where it is.
		event(6, KindAgent, news),
		event(7, KindAssistant, AssistantPayload{Text: "Thanks.", Seen: 6}),
	})

	var shape []string
	for _, m := range got[1:] {
		label := m.Role
		if isNews(m) {
			label = "news"
		}
		shape = append(shape, label)
	}
	if !equal(shape, []string{"user", "assistant", "tool", "assistant", "news", "news", "assistant"}) {
		t.Errorf("conversation = %v; news the model hadn't seen must come after what it wrote", shape)
	}
}

func TestTruncateKeepsBothEnds(t *testing.T) {
	long := "START" + strings.Repeat("x", 20000) + "END"
	got := truncate(long, 1000)

	if len(got) > 1000 || !strings.HasPrefix(got, "START") || !strings.HasSuffix(got, "END") || !strings.Contains(got, "shortened") {
		t.Errorf("truncate: %d bytes, starts %q, ends %q", len(got), got[:10], got[len(got)-10:])
	}
	if truncate("short", 1000) != "short" {
		t.Error("a short string was changed")
	}
}
