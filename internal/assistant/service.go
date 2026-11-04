// Package assistant is the AI built into the platform. A user chats with it;
// it creates machines, hands coding tasks to the agent inside each one, and
// asks the user questions when a choice is theirs to make.
//
// The conversation is an append-only list of events in Postgres. One "run"
// is a loop: send the conversation to the model, carry out the tools it asks
// for, record the results, repeat until it answers in words or asks the user
// something. Runs happen in the background, detached from the HTTP request
// that started them, and anyone watching the chat is told when it changes.
package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/HasanMal1k/coder/internal/store"
)

// Chat statuses.
const (
	StatusIdle    = "idle"
	StatusRunning = "running"
	// StatusWaiting means the assistant asked the user something and the
	// conversation resumes when they answer.
	StatusWaiting = "waiting"
)

// Event kinds. User, assistant and tool events are the conversation the
// model sees; activity and error events are for the person watching.
const (
	KindUser      = "user"
	KindAssistant = "assistant"
	KindTool      = "tool"
	KindActivity  = "activity"
	// KindAgent records that a background agent finished. The model is told
	// of it as if the user had passed the news on.
	KindAgent = "agent"
	// KindMessage records one agent asking another something.
	KindMessage = "message"
	// KindPost records an agent writing in the chat: its answer to the
	// owner, or something it wants to show or ask them.
	KindPost  = "post"
	KindError = "error"
)

const (
	// maxSteps bounds how many times one run may go back to the model, so a
	// model stuck in a loop can't run for ever.
	maxSteps = 40
	// runTimeout bounds a whole run.
	runTimeout = 90 * time.Minute
	// maxMessageLength bounds what a user can send in one message.
	maxMessageLength = 20000
	// maxToolOutput bounds how much of a tool's result is kept and shown to
	// the model. Reports from coding tasks can be long.
	maxToolOutput = 8000
	// maxActivitiesPerTask bounds the progress notes recorded for one task.
	maxActivitiesPerTask = 200

	// draftInterval is the least time between telling watchers that a draft
	// has grown.
	draftInterval = 50 * time.Millisecond

	defaultTitle   = "New chat"
	maxTitleLength = 60
)

var (
	// ErrNotFound is returned when the chat does not exist or belongs to
	// another user.
	ErrNotFound = errors.New("chat not found")
	// ErrBusy is returned when a message arrives while the assistant is
	// still working on the previous one.
	ErrBusy = errors.New("the assistant is still working")
	// ErrDisabled is returned when no model is configured.
	ErrDisabled = errors.New("the assistant is not configured")
	// ErrInvalidMessage is returned for a message that is empty or too long.
	ErrInvalidMessage = errors.New("message is empty or too long")
	// ErrInvalidDesign is returned when a design the user edited doesn't
	// hold together.
	ErrInvalidDesign = errors.New("invalid design")
)

// Machine is what the assistant knows about one of the user's machines.
type Machine struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	CPUs     int    `json:"cpus"`
	MemoryMB int    `json:"memory_mb"`
	DiskGB   int    `json:"disk_gb"`
	// Stack is the toolchain the machine came with.
	Stack string `json:"stack,omitempty"`
}

// MachineSpec describes a machine to create.
type MachineSpec struct {
	Name string
	// Size is the name of a power level; empty means the default.
	Size string
	// DiskGB is the size of the home folder's disk; zero means the default.
	DiskGB int
	// Stack is the toolchain to have installed; empty means the default.
	Stack string
}

// Machines is what the assistant can do with a user's machines. Every method
// acts only on machines belonging to ownerID.
type Machines interface {
	List(ctx context.Context, ownerID string) ([]Machine, error)
	// Create makes a machine and starts it, returning once it is ready for
	// work. progress is called with a line of text for each stage.
	Create(ctx context.Context, ownerID string, spec MachineSpec, progress func(string)) (Machine, error)
	Start(ctx context.Context, ownerID, name string) (Machine, error)
	Stop(ctx context.Context, ownerID, name string) (Machine, error)
	Find(ctx context.Context, ownerID, name string) (Machine, error)
	// RunTask gives a coding task to an agent inside the machine and returns
	// its report, putting files on the machine first. Different agents on
	// one machine work at the same time. progress is called with a line of
	// text for each thing the agent does.
	RunTask(ctx context.Context, ownerID, name string, agent AgentRef, task string, files []TaskFile, progress func(string)) (string, error)
	// GetFile reads a file on a running machine, refusing one larger than
	// maxSize, and returns it with its absolute path.
	GetFile(ctx context.Context, instanceID, path string, maxSize int) ([]byte, string, error)
	// KeepAwake stops the user's machines being shut down for being idle
	// until the returned function is called.
	KeepAwake(ownerID string) (done func())
	// Lookup returns a machine and its owner, for a request that comes
	// from the machine itself or is about it.
	Lookup(ctx context.Context, instanceID string) (ownerID string, m Machine, err error)
	// PortOpen reports whether something on the machine answers on a port.
	PortOpen(ctx context.Context, instanceID string, port int) bool
	// Get asks whatever listens on a machine's port for a path, over HTTP,
	// and returns the status and the start of the body.
	Get(ctx context.Context, instanceID string, port int, path string) (status int, body []byte, err error)
}

// Store is the persistence the assistant needs. *store.ChatRepository
// implements it.
type Store interface {
	Create(ctx context.Context, chat *store.Chat) error
	GetByID(ctx context.Context, id, ownerID string) (*store.Chat, error)
	ListByOwner(ctx context.Context, ownerID string) ([]*store.Chat, error)
	ListByStatus(ctx context.Context, status string) ([]*store.Chat, error)
	TransitionStatus(ctx context.Context, id string, from []string, to string) (bool, error)
	SetStatus(ctx context.Context, id, status string) error
	SetTitle(ctx context.Context, id, title string) error
	Delete(ctx context.Context, id, ownerID string) error
	AppendEvent(ctx context.Context, chatID, kind string, payload json.RawMessage) (*store.ChatEvent, error)
	ListEvents(ctx context.Context, chatID string, after int64) ([]*store.ChatEvent, error)
}

// The payload of each kind of event.
type (
	UserPayload struct {
		Text string `json:"text"`
		// To is who the message was for, by handle, when the owner named
		// agents: "assistant" among them means the assistant too. Empty
		// means the assistant alone.
		To []string `json:"to,omitempty"`
		// Attachments are the files that came with it.
		Attachments []AttachmentRef `json:"attachments,omitempty"`
	}

	AssistantPayload struct {
		Text      string     `json:"text"`
		ToolCalls []ToolCall `json:"tool_calls,omitempty"`
		// Reasoning is the model's thinking before this message. It is not
		// shown; it is sent back to the model with the rest of the history.
		Reasoning string `json:"reasoning,omitempty"`
		// Seen is the last event the model had been shown when it wrote
		// this. Anything between that and this message happened while it
		// was writing, and it has not seen it yet.
		Seen int64 `json:"seen,omitempty"`
	}

	ToolPayload struct {
		ToolCallID string `json:"tool_call_id"`
		Name       string `json:"name"`
		// Content is what the model is told the tool returned.
		Content string `json:"content"`
		// Failed marks a result that is an error, for display.
		Failed bool `json:"failed,omitempty"`
		// Machine, when set, is the machine this result is about, so the
		// interface can link to it. The model never sees it.
		Machine *Machine `json:"machine,omitempty"`
	}

	ActivityPayload struct {
		ToolCallID string `json:"tool_call_id"`
		Machine    string `json:"machine"`
		Text       string `json:"text"`
	}

	ErrorPayload struct {
		Message string `json:"message"`
		// Stopped marks a run the user stopped, as opposed to one that failed.
		Stopped bool `json:"stopped,omitempty"`
	}
)

type Service struct {
	store       Store
	agents      Agents
	attachments Attachments
	designs     Designs
	services    Services
	llm         LLM
	machines    Machines

	mu   sync.Mutex
	live map[string]*liveChat
	// running holds the agents with a task in progress, by agent ID.
	running map[string]*running
	// routes remembers, for a short while, where paths on each machine's
	// ports lead.
	routes map[string]routeTable
	// asked holds, for each user, when their agents lately asked each other
	// things or wrote in a chat, to keep that within bounds (allow).
	asked map[string][]time.Time
	// links reaches machines' link daemons; nil if there are none. meshKeys
	// is the key of each chat's mesh, and linked the peers each machine
	// says it has a stream with.
	links    Links
	meshKeys map[string][]byte
	linked   map[string][]string

	// guests holds what each machine last said when it asked for its
	// record, by instance ID.
	guests map[string]guestState
}

// Stores is the persistence the assistant needs.
type Stores struct {
	Chats       Store
	Agents      Agents
	Attachments Attachments
	Designs     Designs
	Services    Services
}

// liveChat is the in-memory side of a chat: who is watching it, and the run
// in progress, if any.
type liveChat struct {
	// appendMu serializes appends, which the store requires, and which keeps
	// events from tools running at the same time in a sensible order.
	appendMu sync.Mutex

	watchers map[chan struct{}]struct{}
	// cancel stops the run in progress; nil when there is none.
	cancel context.CancelFunc
	// draft is the message the model is in the middle of writing. It is
	// never stored: once complete it becomes an assistant event.
	draft string
	// wakes counts the runs started by background agents finishing since the
	// user last said something; gaveUp is set once that has gone on too long.
	wakes  int
	gaveUp bool
}

// NewService returns an assistant. llm may be nil, in which case chats can be
// read but not continued: Send returns ErrDisabled.
func NewService(stores Stores, llm LLM, machines Machines) *Service {
	return &Service{
		store:       stores.Chats,
		agents:      stores.Agents,
		attachments: stores.Attachments,
		designs:     stores.Designs,
		services:    stores.Services,
		llm:         llm,
		machines:    machines,
		live:        make(map[string]*liveChat),
		running:     make(map[string]*running),
		routes:      make(map[string]routeTable),
		asked:       make(map[string][]time.Time),
	}
}

// Enabled reports whether a model is configured.
func (s *Service) Enabled() bool {
	return s.llm != nil
}

func (s *Service) CreateChat(ctx context.Context, ownerID string) (*store.Chat, error) {
	chat := &store.Chat{
		OwnerID: ownerID,
		Title:   defaultTitle,
		Status:  StatusIdle,
	}
	if err := s.store.Create(ctx, chat); err != nil {
		return nil, err
	}

	return chat, nil
}

func (s *Service) ListChats(ctx context.Context, ownerID string) ([]*store.Chat, error) {
	return s.store.ListByOwner(ctx, ownerID)
}

func (s *Service) GetChat(ctx context.Context, ownerID, id string) (*store.Chat, error) {
	// Anything that isn't a UUID can't be a chat; Postgres would reject it
	// with an error rather than simply finding nothing.
	if uuid.Validate(id) != nil {
		return nil, ErrNotFound
	}

	chat, err := s.store.GetByID(ctx, id, ownerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return chat, nil
}

// Events returns the chat's events after sequence number after.
func (s *Service) Events(ctx context.Context, ownerID, id string, after int64) ([]*store.ChatEvent, error) {
	if _, err := s.GetChat(ctx, ownerID, id); err != nil {
		return nil, err
	}

	return s.store.ListEvents(ctx, id, after)
}

// DeleteChat stops whatever the chat is doing and removes it.
func (s *Service) DeleteChat(ctx context.Context, ownerID, id string) error {
	chat, err := s.GetChat(ctx, ownerID, id)
	if err != nil {
		return err
	}
	// The ID as stored, which is what runs and watchers are kept under.
	id = chat.ID

	s.stopRun(id)
	s.stopChatAgents(ctx, id)

	if err := s.store.Delete(ctx, id, ownerID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.notify(id)

	s.mu.Lock()
	delete(s.live, id)
	s.mu.Unlock()

	return nil
}

// Send adds the user's message to the chat and sets the assistant to work on
// it in the background. If the assistant was waiting for an answer to a
// question, the message is that answer. It is SendMessage with nothing but
// text: a message that mentions agents goes to them instead.
func (s *Service) Send(ctx context.Context, ownerID, id, text string) error {
	return s.SendMessage(ctx, ownerID, id, UserMessage{Text: text})
}

// startRun sets the assistant to work on a chat that has just been claimed.
// The run outlives the request that caused it: it can take minutes, and must
// not stop because a browser tab did.
func (s *Service) startRun(ownerID, id string) {
	runCtx, cancel := context.WithTimeout(context.Background(), runTimeout)
	s.setCancel(id, cancel)

	go func() {
		defer cancel()
		s.run(runCtx, ownerID, id)
	}()
}

// recordMessage stores what the user sent the assistant: as the answer to
// its outstanding question if there is one, and as a new message otherwise.
// It returns the new message's sequence number, or 0 if it was an answer.
func (s *Service) recordMessage(ctx context.Context, chat *store.Chat, msg UserPayload) (int64, error) {
	events, err := s.store.ListEvents(ctx, chat.ID, 0)
	if err != nil {
		return 0, err
	}

	// Whatever the assistant is waiting on, this is the reply to it: the
	// answer to a question, or what to change in a design it proposed.
	pending := unanswered(events, toolAskUser, toolProposeArchitecture)
	if len(pending) == 0 {
		if chat.Title == defaultTitle {
			named := msg.Text
			if named == "" && len(msg.Attachments) > 0 {
				named = msg.Attachments[0].Name
			}
			if err := s.store.SetTitle(ctx, chat.ID, title(named)); err != nil {
				return 0, err
			}
		}
		event, err := s.appendEvent(ctx, chat.ID, KindUser, msg)
		if err != nil {
			return 0, err
		}
		return event.Seq, nil
	}

	text := msg.Text + describeAttachments(msg.Attachments)
	for i, call := range pending {
		answer := text
		switch {
		case i > 0:
			// The model asked several things at once and got one reply.
			answer = "(The user replied once, to all of this; see their answer above.)"
		case call.Function.Name == toolProposeArchitecture:
			answer = "The user has not approved the design. They said:\n\n" + text + "\n\nIf they asked for a change, make it and propose the whole design again. If they asked a question, answer it and propose again."
		}

		err := s.append(ctx, chat.ID, KindTool, ToolPayload{
			ToolCallID: call.ID,
			Name:       call.Function.Name,
			Content:    answer,
		})
		if err != nil {
			return 0, err
		}
	}

	return 0, nil
}

// Cancel stops the assistant if it is working, and withdraws its question if
// it was waiting on one.
func (s *Service) Cancel(ctx context.Context, ownerID, id string) error {
	chat, err := s.GetChat(ctx, ownerID, id)
	if err != nil {
		return err
	}
	id = chat.ID

	if s.stopRun(id) {
		// The run notices, records that it was stopped, and frees the chat.
		return nil
	}

	released, err := s.store.TransitionStatus(ctx, id, []string{StatusWaiting, StatusRunning}, StatusIdle)
	if err != nil {
		return err
	}
	if released {
		s.notify(id)
	}

	return nil
}

// RecoverInterrupted frees chats and agents that were mid-task when the
// server last stopped. Call it once at startup: runs only happen inside this process, so
// any chat still marked running was cut off.
func (s *Service) RecoverInterrupted(ctx context.Context) (int, error) {
	chats, err := s.store.ListByStatus(ctx, StatusRunning)
	if err != nil {
		return 0, err
	}

	for _, chat := range chats {
		err := s.append(ctx, chat.ID, KindError, ErrorPayload{
			Message: "The server restarted while I was working, so I stopped part-way. Send a message to carry on.",
		})
		if err != nil {
			return 0, err
		}
		if err := s.store.SetStatus(ctx, chat.ID, StatusIdle); err != nil {
			return 0, err
		}
	}

	// The same goes for agents: one still marked as working has nothing
	// behind it any more.
	agents, err := s.agents.ListByStatus(ctx, AgentWorking)
	if err != nil {
		return 0, err
	}

	const interrupted = "The server restarted while this agent was working, so its task was cut short. What it had done so far is still on the machine."
	for _, agent := range agents {
		if err := s.agents.Finish(ctx, agent.ID, AgentFailed, interrupted); err != nil {
			return 0, err
		}
		// A background task has no run waiting on it; its chat is told here,
		// so that it doesn't show the agent as working for ever.
		if agent.Background {
			s.append(ctx, agent.ChatID, KindAgent, AgentPayload{
				AgentID: agent.ID,
				Agent:   agent.Name,
				Handle:  agent.Handle,
				Machine: Machine{ID: agent.InstanceID, Name: agent.MachineName},
				Content: "Error: " + interrupted,
				Failed:  true,
			})
		}
	}

	return len(chats), nil
}

// Draft returns what the assistant has written so far of the message it is
// composing, or "" if it isn't composing one. The finished message arrives as
// an event; this is only for showing it take shape.
func (s *Service) Draft(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if live, ok := s.live[id]; ok {
		return live.draft
	}

	return ""
}

// Watch returns a channel that receives a value whenever the chat changes
// (a new event, a new status, or more of a draft), and a function to stop
// watching. The
// channel says only that something changed; read the chat to find out what.
func (s *Service) Watch(id string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)

	s.mu.Lock()
	live := s.liveLocked(id)
	live.watchers[ch] = struct{}{}
	s.mu.Unlock()

	return ch, func() {
		s.mu.Lock()
		delete(live.watchers, ch)
		s.mu.Unlock()
	}
}

// run is one turn of the assistant: it goes round the loop of asking the
// model and carrying out its tools until the model answers in words, asks
// the user something, or something stops it.
func (s *Service) run(ctx context.Context, ownerID, chatID string) {
	// The chat's status is updated on a context that can't be cancelled:
	// however the run ends, the chat must not be left marked as running.
	background := context.WithoutCancel(ctx)

	// The user may well close the app and come back later; their machines
	// have to stay up for the assistant in the meantime.
	awake := s.machines.KeepAwake(ownerID)
	defer awake()

	final := StatusIdle
	// seen is the last event the model has been shown; answered is whether
	// the run ended with the model saying it was done.
	var (
		seen     int64
		answered bool
	)
	defer func() {
		s.setCancel(chatID, nil)
		if err := s.store.SetStatus(background, chatID, final); err != nil {
			return
		}
		s.notify(chatID)

		// A background agent may have finished while the model was writing
		// its last message, too late to be part of what it was shown. Then
		// the assistant has more to deal with after all.
		if answered && s.newsSince(background, chatID, seen) {
			s.wake(background, ownerID, chatID)
		}
	}()

	fail := func(err error) {
		payload := ErrorPayload{Message: err.Error()}
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			payload = ErrorPayload{Message: "Stopped.", Stopped: true}
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			payload = ErrorPayload{Message: "I ran out of time and stopped."}
		}
		s.append(background, chatID, KindError, payload)
	}

	for step := 0; step < maxSteps; step++ {
		events, err := s.store.ListEvents(ctx, chatID, 0)
		if err != nil {
			fail(err)
			return
		}
		if len(events) > 0 {
			seen = events[len(events)-1].Seq
		}

		reply, err := s.llm.Complete(ctx, history(events), toolDefs, s.drafting(chatID))
		// Whatever was drafted is now either the reply below or abandoned.
		s.setDraft(chatID, "")
		if err != nil {
			fail(err)
			return
		}

		err = s.append(ctx, chatID, KindAssistant, AssistantPayload{
			Text:      strings.TrimSpace(reply.Content),
			ToolCalls: reply.ToolCalls,
			Reasoning: reply.ReasoningContent,
			Seen:      seen,
		})
		if err != nil {
			fail(err)
			return
		}

		if len(reply.ToolCalls) == 0 {
			// The model answered in words: the turn is over.
			answered = true
			return
		}

		asked := s.carryOut(ctx, ownerID, chatID, reply.ToolCalls)
		if ctx.Err() != nil {
			fail(ctx.Err())
			return
		}
		if asked {
			// A question is outstanding. Its answer arrives through Send,
			// which starts the next run.
			final = StatusWaiting
			return
		}
	}

	fail(fmt.Errorf("I stopped after %d steps without finishing. Tell me how you'd like to continue.", maxSteps))
}

// newsSince reports whether a background agent has finished since event
// number seq.
func (s *Service) newsSince(ctx context.Context, chatID string, seq int64) bool {
	events, err := s.store.ListEvents(ctx, chatID, seq)
	if err != nil {
		return false
	}

	for _, event := range events {
		if event.Kind == KindAgent {
			return true
		}
	}

	return false
}

// carryOut runs the tools the model asked for, all at once, and records each
// result. It reports whether the model asked the user a question, which is
// not answered here.
func (s *Service) carryOut(ctx context.Context, ownerID, chatID string, calls []ToolCall) (asked bool) {
	var wg sync.WaitGroup

	for _, call := range calls {
		if call.Function.Name == toolAskUser {
			asked = true
			continue
		}
		if call.Function.Name == toolProposeArchitecture {
			// A design that holds together is put to the user and waited
			// on. One that doesn't goes straight back to the model to fix.
			if err := s.propose(ctx, ownerID, chatID, call); err != nil {
				s.append(context.WithoutCancel(ctx), chatID, KindTool, ToolPayload{
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Content:    "Error: " + err.Error(),
					Failed:     true,
				})
			} else {
				asked = true
			}
			continue
		}

		wg.Go(func() {
			result := s.execute(ctx, ownerID, chatID, call)
			// Recorded even if the run is being stopped, so the history
			// never has a request without a result.
			s.append(context.WithoutCancel(ctx), chatID, KindTool, result)
		})
	}
	wg.Wait()

	return asked
}

// drafting returns the function a model client calls with each piece of text
// as it is written. Watchers are told as the draft grows, but no more often
// than draftInterval: a model can produce dozens of pieces a second.
func (s *Service) drafting(chatID string) func(string) {
	var told time.Time

	return func(piece string) {
		s.mu.Lock()
		live := s.liveLocked(chatID)
		live.draft += piece
		s.mu.Unlock()

		if time.Since(told) >= draftInterval {
			told = time.Now()
			s.notify(chatID)
		}
	}
}

func (s *Service) setDraft(chatID, draft string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.liveLocked(chatID).draft = draft
}

// append records an event and tells whoever is watching.
func (s *Service) append(ctx context.Context, chatID, kind string, payload any) error {
	_, err := s.appendEvent(ctx, chatID, kind, payload)
	return err
}

// appendEvent is append, returning the event recorded.
func (s *Service) appendEvent(ctx context.Context, chatID, kind string, payload any) (*store.ChatEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode %s event: %w", kind, err)
	}

	s.mu.Lock()
	live := s.liveLocked(chatID)
	s.mu.Unlock()

	live.appendMu.Lock()
	event, err := s.store.AppendEvent(ctx, chatID, kind, raw)
	live.appendMu.Unlock()
	if err != nil {
		return nil, err
	}

	s.notify(chatID)

	return event, nil
}

func (s *Service) notify(chatID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for ch := range s.liveLocked(chatID).watchers {
		// Non-blocking: a watcher that already has a wake-up pending will
		// see this change too when it looks.
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *Service) setCancel(chatID string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.liveLocked(chatID).cancel = cancel
}

// stopRun cancels the chat's run, if it has one, and reports whether it did.
func (s *Service) stopRun(chatID string) bool {
	s.mu.Lock()
	cancel := s.liveLocked(chatID).cancel
	s.mu.Unlock()

	if cancel == nil {
		return false
	}
	cancel()

	return true
}

// liveLocked returns the chat's in-memory state, creating it if needed. The
// caller must hold s.mu.
func (s *Service) liveLocked(chatID string) *liveChat {
	live, ok := s.live[chatID]
	if !ok {
		live = &liveChat{watchers: make(map[chan struct{}]struct{})}
		s.live[chatID] = live
	}

	return live
}

// history turns a chat's events into the conversation the model is sent.
func history(events []*store.ChatEvent) []Message {
	messages := []Message{{Role: "system", Content: systemPrompt}}

	// News from background agents is stored when it arrives, which can be
	// while the model is writing a message, and so before that message. The
	// model hadn't seen it then; it is shown after that message instead, as
	// news still to deal with.
	seenBy := make([]int64, len(events)) // what the next assistant message had seen
	next := int64(-1)
	for i := len(events) - 1; i >= 0; i-- {
		seenBy[i] = next
		if events[i].Kind == KindAssistant {
			var p AssistantPayload
			json.Unmarshal(events[i].Payload, &p)
			next = p.Seen
			if next == 0 {
				// Written before this was recorded: take the order as it is.
				next = events[i].Seq
			}
		}
	}

	var late []Message
	// news adds something that happened beside the conversation: after the
	// message the model was writing when it happened, if it hadn't seen it.
	news := func(i int, m Message) {
		if seenBy[i] >= 0 && seenBy[i] < events[i].Seq {
			late = append(late, m)
		} else {
			messages = append(messages, m)
		}
	}

	for i, event := range events {
		switch event.Kind {
		case KindUser:
			var p UserPayload
			json.Unmarshal(event.Payload, &p)
			content := p.Text + describeAttachments(p.Attachments)
			if len(p.To) > 0 && !slices.Contains(p.To, assistantHandle) {
				// Written to agents, not to the assistant, and possibly
				// while it was working: it reads it, and need not act.
				news(i, Message{Role: "user", Content: directNews(p.To, content)})
				continue
			}
			if len(p.To) > 0 {
				content = fmt.Sprintf("[The user wrote this to you and to %s, who are answering it themselves.]\n\n%s", handlesText(p.To), content)
			}
			messages = append(messages, Message{Role: "user", Content: content})

		case KindPost:
			var p PostPayload
			json.Unmarshal(event.Payload, &p)
			if p.Queued {
				continue
			}
			news(i, Message{Role: "user", Content: postNews(p)})

		case KindAssistant:
			var p AssistantPayload
			json.Unmarshal(event.Payload, &p)
			messages = append(messages, Message{Role: "assistant", Content: p.Text, ReasoningContent: p.Reasoning, ToolCalls: p.ToolCalls})
			messages = append(messages, late...)
			late = nil

		case KindTool:
			var p ToolPayload
			json.Unmarshal(event.Payload, &p)
			messages = append(messages, Message{Role: "tool", ToolCallID: p.ToolCallID, Content: p.Content})

		case KindAgent:
			// The call that started the agent was answered long ago ("it is
			// working"), so the outcome can't be that call's result. It
			// reaches the model as a message instead.
			var p AgentPayload
			json.Unmarshal(event.Payload, &p)
			news(i, Message{Role: "user", Content: agentNews(p)})
		}
	}

	return repair(append(messages, late...))
}

// agentNews words the outcome of a background agent's task for the model. It
// is marked as coming from the platform, so the model doesn't take it for
// something the user typed.
func agentNews(p AgentPayload) string {
	switch {
	case p.Stopped:
		return fmt.Sprintf("[From the platform, not the user] The user stopped background agent %q on machine %q before it finished. What it had done so far is still there.", p.Agent, p.Machine.Name)
	case p.Failed:
		return fmt.Sprintf("[From the platform, not the user] Background agent %q on machine %q failed.\n\n%s", p.Agent, p.Machine.Name, p.Content)
	default:
		return fmt.Sprintf("[From the platform, not the user] Background agent %q on machine %q has finished. Its report:\n\n%s", p.Agent, p.Machine.Name, p.Content)
	}
}

// directNews tells the model of a message the user wrote to agents, not to
// it.
func directNews(to []string, content string) string {
	return fmt.Sprintf("[From the platform, not to you] The user wrote this in the chat to %s directly, who will answer it themselves. You need not act on it unless it changes what you are doing.\n\n%s", handlesText(to), content)
}

// postNews tells the model of something an agent wrote in the chat.
func postNews(p PostPayload) string {
	who := fmt.Sprintf("%s (the %q agent on %q)", p.Agent, p.Role, p.Machine.Name)

	var b strings.Builder
	switch {
	case p.Failed:
		fmt.Fprintf(&b, "[From the platform, not the user] %s could not answer the user's message: %s", who, p.Text)
	case p.ReplyTo != 0:
		fmt.Fprintf(&b, "[From the platform, not the user] %s answered the user in the chat:\n\n%s", who, p.Text)
	case p.Question:
		fmt.Fprintf(&b, "[From the platform, not the user] %s asked the user in the chat:\n\n%s", who, p.Text)
		if len(p.Options) > 0 {
			fmt.Fprintf(&b, "\n\nOptions: %s", strings.Join(p.Options, "; "))
		}
		b.WriteString("\n\nThe user's answer goes to that agent, not to you.")
	default:
		fmt.Fprintf(&b, "[From the platform, not the user] %s wrote in the chat:\n\n%s", who, p.Text)
	}
	for _, a := range p.Attachments {
		fmt.Fprintf(&b, "\n[With a file: %s (%s, %s), id %s]", a.Name, a.ContentType, sizeText(a.Size), a.ID)
	}
	if p.Preview != nil {
		fmt.Fprintf(&b, "\n[With a button opening port %d%s on %q]", p.Preview.Port, p.Preview.Path, p.Machine.Name)
	}

	return b.String()
}

// handlesText lists agents by handle, leaving out the assistant: "@tom and
// @ava".
func handlesText(handles []string) string {
	var named []string
	for _, h := range handles {
		if h != assistantHandle {
			named = append(named, "@"+h)
		}
	}

	switch len(named) {
	case 0:
		return "nobody"
	case 1:
		return named[0]
	}
	return strings.Join(named[:len(named)-1], ", ") + " and " + named[len(named)-1]
}

// repair makes a conversation acceptable to the model. The API rejects a
// conversation in which the assistant requested a tool and no result
// follows, which is what a run cut short leaves behind; and it wants results
// directly after the request. So every request gets its results placed right
// after it, with a stand-in for any that never arrived.
func repair(messages []Message) []Message {
	results := make(map[string]Message)
	for _, m := range messages {
		if m.Role == "tool" {
			results[m.ToolCallID] = m
		}
	}

	out := make([]Message, 0, len(messages))
	for _, m := range messages {
		if m.Role == "tool" {
			continue
		}
		out = append(out, m)

		for _, call := range m.ToolCalls {
			result, ok := results[call.ID]
			if !ok {
				result = Message{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    "This was interrupted before it finished, and has no result.",
				}
			}
			out = append(out, result)
		}
	}

	return out
}

// unanswered returns the calls to the named tools that have no result yet.
func unanswered(events []*store.ChatEvent, names ...string) []ToolCall {
	answered := make(map[string]bool)
	var calls []ToolCall

	for _, event := range events {
		switch event.Kind {
		case KindAssistant:
			var p AssistantPayload
			json.Unmarshal(event.Payload, &p)
			for _, call := range p.ToolCalls {
				if slices.Contains(names, call.Function.Name) {
					calls = append(calls, call)
				}
			}
		case KindTool:
			var p ToolPayload
			json.Unmarshal(event.Payload, &p)
			answered[p.ToolCallID] = true
		}
	}

	var pending []ToolCall
	for _, call := range calls {
		if !answered[call.ID] {
			pending = append(pending, call)
		}
	}

	return pending
}

// title makes a chat title from the user's first message.
func title(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	line = strings.Join(strings.Fields(line), " ")

	runes := []rune(line)
	if len(runes) > maxTitleLength {
		return strings.TrimSpace(string(runes[:maxTitleLength-1])) + "…"
	}
	if line == "" {
		return defaultTitle
	}

	return line
}
