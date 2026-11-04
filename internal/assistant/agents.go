package assistant

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/HasanMal1k/coder/internal/store"
)

// Agent statuses. An agent is a coding agent inside a machine: it has a name,
// remembers the tasks it was given before, and works on one task at a time.
// A machine can have several, working at once.
const (
	AgentIdle    = "idle"
	AgentWorking = "working"
	AgentFailed  = "failed"
	AgentStopped = "stopped"
)

const (
	// defaultAgentName is the agent a task goes to when none is named.
	defaultAgentName = "coder"
	// askTimeout bounds one agent's answer to another's question.
	askTimeout = 5 * time.Minute
	// maxQuestion bounds a question's length.
	maxQuestion = 4000
	// Agents asking each other set each other to work, with nobody
	// deciding it should happen. This bounds how often, for one user:
	// askLimit questions within askWindow.
	askLimit  = 30
	askWindow = 10 * time.Minute

	// maxWakes bounds how many times in a row the assistant may be set going
	// by background agents finishing, with the user saying nothing in
	// between. Agents that finish can cause more agents to be started; this
	// is what keeps that from going on for ever.
	maxWakes = 20
)

var (
	// ErrAgentNotFound is returned when the agent does not exist or belongs
	// to another user.
	ErrAgentNotFound = errors.New("agent not found")

	// errAgentStopped is what a task ends with when the user stops its agent.
	errAgentStopped = errors.New("the user stopped this agent")
)

// Agent names follow the same rule as machine names: short, lowercase, and
// safe to show anywhere.
var validAgentName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)

// AgentRef says which agent a task is for.
type AgentRef struct {
	// ID identifies the agent's conversation, so it keeps its memory from
	// one task to the next.
	ID string
	// Name is the agent's job and Handle its own name, for telling it who
	// it is.
	Name   string
	Handle string
}

// Agents is the persistence agents need. *store.AgentRepository implements
// it.
type Agents interface {
	// Ensure creates an agent called handle, or returns the one there is;
	// store.ErrHandleTaken if another of the owner's agents has handle.
	Ensure(ctx context.Context, ownerID, chatID, instanceID, name, handle string) (*store.Agent, error)
	Handles(ctx context.Context, ownerID string) ([]string, error)
	GetByID(ctx context.Context, id, ownerID string) (*store.Agent, error)
	ListByOwner(ctx context.Context, ownerID string) ([]*store.Agent, error)
	ListByChat(ctx context.Context, chatID string) ([]*store.Agent, error)
	ListByStatus(ctx context.Context, status string) ([]*store.Agent, error)
	Begin(ctx context.Context, id, task string, background bool) (bool, error)
	Finish(ctx context.Context, id, status, report string) error
	SetActivity(ctx context.Context, id, activity string) error
	// ListTasks returns the latest tasks of each of the owner's agents.
	ListTasks(ctx context.Context, ownerID string, perAgent int) ([]*store.AgentTask, error)
	AddNote(ctx context.Context, note *store.AgentNote) error
	ListNotes(ctx context.Context, ownerID string, limit int) ([]*store.AgentNote, error)
	// AddMessage leaves a message for an agent that is busy; TakeMessages
	// hands over those waiting for it, from the owner or from agents, once.
	AddMessage(ctx context.Context, toAgentID string, msg *store.AgentMessage) error
	TakeMessages(ctx context.Context, agentID string, fromOwner bool) ([]*store.AgentMessage, error)
}

// AgentPayload is the payload of an "agent" event: a background agent has
// finished the task it was given.
type AgentPayload struct {
	AgentID string `json:"agent_id"`
	Agent   string `json:"agent"`
	// Handle is the agent's own name.
	Handle string `json:"handle,omitempty"`
	// ToolCallID is the call that set the agent going, so the interface can
	// show the outcome where the task was started.
	ToolCallID string  `json:"tool_call_id"`
	Machine    Machine `json:"machine"`
	// Content is the agent's report, or why it failed.
	Content string `json:"content"`
	Failed  bool   `json:"failed,omitempty"`
	// Stopped marks a task the user stopped, as opposed to one that failed.
	Stopped bool `json:"stopped,omitempty"`
}

// job is one task for one agent.
type job struct {
	ownerID string
	chatID  string
	// callID is the tool call the task came from.
	callID  string
	machine Machine
	agent   *store.Agent
	task    string
	// files are put on the machine before the task is given.
	files      []TaskFile
	background bool
}

// running is the in-memory side of an agent at work.
type running struct {
	cancel context.CancelFunc
	// stopped is set when the user stops the agent, to tell that apart from
	// its chat being stopped or its task failing.
	stopped bool
}

// ListAgents returns the user's agents, those working first.
func (s *Service) ListAgents(ctx context.Context, ownerID string) ([]*store.Agent, error) {
	return s.agents.ListByOwner(ctx, ownerID)
}

// StopAgent stops the agent's task, if it is working on one.
func (s *Service) StopAgent(ctx context.Context, ownerID, id string) error {
	if uuid.Validate(id) != nil {
		return ErrAgentNotFound
	}

	agent, err := s.agents.GetByID(ctx, id, ownerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrAgentNotFound
		}
		return err
	}

	if s.cancelAgent(agent.ID, true) {
		// Its task notices, tells the agent program to stop, and records how
		// it ended.
		return nil
	}

	// Marked as working with no task behind it in this process. Nothing to
	// stop; only the record to put right.
	if agent.Status == AgentWorking {
		return s.agents.Finish(ctx, agent.ID, AgentStopped, "Stopped.")
	}

	return nil
}

// stopChatAgents stops every agent of a chat that is working.
func (s *Service) stopChatAgents(ctx context.Context, chatID string) {
	agents, err := s.agents.ListByChat(ctx, chatID)
	if err != nil {
		return
	}

	for _, agent := range agents {
		s.cancelAgent(agent.ID, true)
	}
}

// cancelAgent cancels the agent's task in progress, and reports whether there
// was one. byUser says the user asked for it.
func (s *Service) cancelAgent(agentID string, byUser bool) bool {
	s.mu.Lock()
	run, ok := s.running[agentID]
	if ok && byUser {
		run.stopped = true
	}
	s.mu.Unlock()

	if !ok {
		return false
	}
	run.cancel()

	return true
}

// assign gives a task to an agent of the chat, creating the agent if this is
// its first. In the foreground it returns the agent's report once the task is
// done. In the background it returns at once, and the chat is told when the
// agent finishes.
func (s *Service) assign(ctx context.Context, ownerID, chatID, callID string, m Machine, name, task string, files []TaskFile, background bool) (string, error) {
	if name == "" {
		name = defaultAgentName
	}
	if !validAgentName.MatchString(name) {
		return "", fmt.Errorf("%q is not a valid agent name: use 1-40 lowercase letters, digits or hyphens, such as \"backend\" or \"tester\"", name)
	}

	agent, err := s.ensureAgent(ctx, ownerID, chatID, m.ID, name)
	if err != nil {
		return "", err
	}
	name = agent.Name

	// Claim the agent. One agent does one thing at a time; two tasks for the
	// same agent would be two sets of edits to the same conversation.
	claimed, err := s.agents.Begin(ctx, agent.ID, task, background)
	if err != nil {
		return "", err
	}
	if !claimed {
		return "", fmt.Errorf("agent %q on %q is still working on its last task. Give this task to another agent on that machine (use a different agent name), or wait for %q to finish", name, m.Name, name)
	}

	j := job{
		ownerID:    ownerID,
		chatID:     chatID,
		callID:     callID,
		machine:    m,
		agent:      agent,
		task:       task,
		files:      files,
		background: background,
	}

	if !background {
		report, err := s.work(ctx, j)
		s.followUp(ownerID, chatID, agent)
		return report, err
	}

	// On its own from here: it outlives the run that started it, and the
	// request before that.
	go func() {
		report, err := s.work(context.WithoutCancel(ctx), j)
		s.finished(j, report, err)
		s.followUp(ownerID, chatID, agent)
	}()

	return fmt.Sprintf("Agent %q (the user calls it @%s) is now working on %q in the background. You will be sent a message when it finishes, so do not wait for it or check on it; carry on with anything else, or end your turn.", name, agent.Handle, m.Name), nil
}

// ensureAgent returns the chat's agent with the given job on a machine,
// creating it with a handle of its own if there is none. A name that is the
// handle of one of the chat's agents is that agent.
func (s *Service) ensureAgent(ctx context.Context, ownerID, chatID, instanceID, name string) (*store.Agent, error) {
	agents, err := s.agents.ListByChat(ctx, chatID)
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		if agent.InstanceID == instanceID && (agent.Name == name || agent.Handle == name) {
			return agent, nil
		}
	}

	// Two agents made at once may both be offered the same handle; the one
	// that loses tries the next.
	for range 5 {
		taken, err := s.agents.Handles(ctx, ownerID)
		if err != nil {
			return nil, err
		}

		agent, err := s.agents.Ensure(ctx, ownerID, chatID, instanceID, name, nextHandle(append(taken, assistantHandle)))
		if errors.Is(err, store.ErrHandleTaken) {
			continue
		}
		return agent, err
	}

	return nil, fmt.Errorf("could not find a free name for agent %q", name)
}

// work carries out a job and records how it ended.
func (s *Service) work(ctx context.Context, j job) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	run := &running{cancel: cancel}
	s.mu.Lock()
	s.running[j.agent.ID] = run
	s.mu.Unlock()

	// While an agent works for a user, their machines stay up: it may well
	// need one of the others.
	awake := s.machines.KeepAwake(j.ownerID)
	defer awake()

	// Bookkeeping outlives the task: it must be recorded however it ended.
	background := context.WithoutCancel(ctx)

	// A task that came from a tool call shows its steps under that call. A
	// question from another agent has no call to show them under.
	notes := func(string) {}
	if j.callID != "" {
		notes = s.progress(ctx, j.chatID, j.callID, j.machine.Name)
	}
	progress := func(text string) {
		s.agents.SetActivity(background, j.agent.ID, text)
		notes(text)
	}

	agent := AgentRef{ID: j.agent.ID, Name: j.agent.Name, Handle: j.agent.Handle}
	report, err := s.machines.RunTask(ctx, j.ownerID, j.machine.Name, agent, s.withMessages(ctx, j), j.files, progress)

	s.mu.Lock()
	delete(s.running, j.agent.ID)
	stopped := run.stopped
	s.mu.Unlock()

	status, outcome := AgentIdle, report
	switch {
	case err == nil:
		if strings.TrimSpace(report) == "" {
			report = "The coding agent finished without saying anything."
			outcome = report
		}
	case stopped:
		err = errAgentStopped
		status, outcome = AgentStopped, "Stopped."
	case errors.Is(err, context.Canceled):
		// Its chat was stopped, and the task with it.
		status, outcome = AgentStopped, "Stopped."
	default:
		status, outcome = AgentFailed, err.Error()
	}

	if finishErr := s.agents.Finish(background, j.agent.ID, status, truncate(outcome, maxToolOutput)); finishErr != nil && err == nil {
		err = finishErr
	}

	return report, err
}

// withMessages returns the job's task with, in front of it, anything other
// agents asked this one while it was busy. They are handed over once.
func (s *Service) withMessages(ctx context.Context, j job) string {
	messages, err := s.agents.TakeMessages(ctx, j.agent.ID, false)
	if err != nil || len(messages) == 0 {
		return j.task
	}

	var b strings.Builder
	b.WriteString("While you were busy, other agents left you these questions. Answer each by leaving a note with share_note, giving the question's subject as the topic, then carry on with your task.\n\n")
	for _, m := range messages {
		fmt.Fprintf(&b, "- From %q on machine %q: %s\n", m.FromAgent, m.FromMachine, oneLine(m.Body))
	}
	b.WriteString("\nYour task:\n\n" + j.task)

	return b.String()
}

// Peer names an agent: which machine it is on and what it is called.
type Peer struct {
	Machine string `json:"machine"`
	Agent   string `json:"agent"`
}

// MessagePayload is the payload of a "message" event: one agent asked
// another something.
type MessagePayload struct {
	From     Peer   `json:"from"`
	To       Peer   `json:"to"`
	Question string `json:"question"`
	// Answer is what the other agent said. Empty when it was busy and the
	// question was left for it.
	Answer string `json:"answer,omitempty"`
	Queued bool   `json:"queued,omitempty"`
	Failed bool   `json:"failed,omitempty"`
}

// questionTask is what an agent is given when another asks it something.
const questionTask = `Another agent, %q on machine %q, is asking you something. Answer from what you built and what is on this machine: be specific, with ports, paths, names and the exact shape of any data. Look at your files if you need to be sure. Do not change anything unless the question asks you to, and do not pass the question on to yet another agent.

The question:

%s`

// AskAgent puts one agent's question to another, and returns the answer.
//
// If the other agent is free it is set to work on the question there and
// then, in its own conversation, with everything it remembers; its reply is
// the answer. If it is busy, the question is left for it to find with its
// next task, and what is recorded about it so far is returned in place of
// an answer.
//
// fromMachine is the machine the question came from, which is known for
// certain; fromAgent is the asking agent's name, as it gave it.
func (s *Service) AskAgent(ctx context.Context, ownerID, fromMachine, fromAgent, toMachine, toAgent, question string) (string, error) {
	fromAgent, toMachine, toAgent = strings.TrimSpace(fromAgent), strings.TrimSpace(toMachine), strings.TrimSpace(toAgent)
	question = strings.TrimSpace(question)

	if fromAgent == "" || question == "" || len(question) > maxQuestion {
		return "", Mistake(fmt.Sprintf("Asking needs your own name (agent_name), and a question of at most %d characters.", maxQuestion))
	}
	if fromMachine == toMachine && fromAgent == toAgent {
		return "", Mistake("That is you. Ask another agent; list_agents shows who there is.")
	}

	agents, err := s.agents.ListByOwner(ctx, ownerID)
	if err != nil {
		return "", err
	}
	var target *store.Agent
	for _, a := range agents {
		if a.MachineName == toMachine && (a.Name == toAgent || a.Handle == toAgent) {
			target = a
			break
		}
	}
	if target == nil {
		var known []string
		for _, a := range agents {
			known = append(known, fmt.Sprintf("%s (%s) on %s", a.Handle, a.Name, a.MachineName))
		}
		return "", Mistake(fmt.Sprintf("There is no agent called %q on %q. The agents there are: %s.", toAgent, toMachine, strings.Join(known, ", ")))
	}

	if !s.mayAsk(ownerID) {
		return "", Mistake(fmt.Sprintf("Agents have asked each other more than %d questions in the last %s. Use get_agent and read_notes to look things up instead, and carry on with what you know.", askLimit, askWindow))
	}

	exchange := MessagePayload{
		From:     Peer{Machine: fromMachine, Agent: fromAgent},
		To:       Peer{Machine: toMachine, Agent: toAgent},
		Question: question,
	}
	task := fmt.Sprintf(questionTask, fromAgent, fromMachine, question)

	claimed, err := s.agents.Begin(ctx, target.ID, task, false)
	if err != nil {
		return "", err
	}
	if !claimed {
		// Busy. The question waits for it; the asker gets what is known.
		err := s.agents.AddMessage(ctx, target.ID, &store.AgentMessage{FromMachine: fromMachine, FromAgent: fromAgent, Body: question})
		if err != nil {
			return "", err
		}
		exchange.Queued = true
		s.append(ctx, target.ChatID, KindMessage, exchange)

		known := "Nothing is recorded about it yet."
		if team, err := s.Team(ctx, ownerID); err == nil {
			if a := team.Agent(toMachine, toAgent); a != nil {
				known = truncate(a.Detail(), maxToolOutput)
			}
		}
		return fmt.Sprintf("%q on %q is in the middle of a task, so it can't answer now. Your question has been left for it; it will answer with a note (read_notes) when it gets to it. Meanwhile, this is what is recorded about it:\n\n%s", toAgent, toMachine, known), nil
	}

	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()

	answer, err := s.work(ctx, job{
		ownerID: ownerID,
		chatID:  target.ChatID,
		machine: Machine{ID: target.InstanceID, Name: target.MachineName},
		agent:   target,
		task:    task,
	})
	s.followUp(ownerID, target.ChatID, target)
	if err != nil {
		exchange.Failed, exchange.Answer = true, err.Error()
		s.append(context.WithoutCancel(ctx), target.ChatID, KindMessage, exchange)
		return "", Mistake(fmt.Sprintf("%q on %q could not answer: %v. Look it up with get_agent instead.", toAgent, toMachine, err))
	}

	exchange.Answer = truncate(answer, maxToolOutput)
	s.append(context.WithoutCancel(ctx), target.ChatID, KindMessage, exchange)

	return exchange.Answer, nil
}

// mayAsk reports whether the user's agents may ask each other another
// question now, and counts it if so.
func (s *Service) mayAsk(ownerID string) bool {
	return s.allow(ownerID, askLimit, askWindow)
}

// finished tells a chat that one of its background agents is done, and sets
// the assistant going again to deal with it.
func (s *Service) finished(j job, report string, err error) {
	ctx := context.Background()

	payload := AgentPayload{
		AgentID:    j.agent.ID,
		Agent:      j.agent.Name,
		Handle:     j.agent.Handle,
		ToolCallID: j.callID,
		Machine:    j.machine,
		Content:    truncate(report, maxToolOutput),
	}
	if err != nil {
		payload.Content = "Error: " + err.Error()
		payload.Failed = true
		payload.Stopped = errors.Is(err, errAgentStopped) || errors.Is(err, context.Canceled)
	}

	// Fails if the chat was deleted meanwhile, in which case there is nobody
	// to tell.
	if s.append(ctx, j.chatID, KindAgent, payload) != nil {
		return
	}

	s.wake(ctx, j.ownerID, j.chatID)
}

// wake starts a run for a chat that has news from its background agents, if
// it isn't already running. A chat that is waiting for the user's answer is
// left waiting: the news is in its history for when they do answer.
func (s *Service) wake(ctx context.Context, ownerID, chatID string) {
	if s.llm == nil {
		return
	}

	s.mu.Lock()
	live := s.liveLocked(chatID)
	over := live.wakes >= maxWakes
	tell := over && !live.gaveUp
	if tell {
		live.gaveUp = true
	}
	s.mu.Unlock()

	if over {
		if tell {
			s.append(ctx, chatID, KindError, ErrorPayload{
				Message: fmt.Sprintf("Background agents have set me going %d times in a row, so I'll stop reacting to them. Send a message to carry on.", maxWakes),
			})
		}
		return
	}

	claimed, err := s.store.TransitionStatus(ctx, chatID, []string{StatusIdle}, StatusRunning)
	if err != nil || !claimed {
		// Already running: that run looks for news when it ends.
		return
	}
	s.notify(chatID)

	// Counted only once a run is actually started.
	s.mu.Lock()
	live.wakes++
	s.mu.Unlock()

	s.startRun(ownerID, chatID)
}

// describeAgent says in a line what an agent is doing, for the model.
func describeAgent(a *store.Agent, now time.Time) string {
	line := fmt.Sprintf("- %q (@%s) on %s: %s", a.Name, a.Handle, a.MachineName, a.Status)

	switch a.Status {
	case AgentWorking:
		if a.StartedAt != nil {
			line += fmt.Sprintf(" for %s", now.Sub(*a.StartedAt).Round(time.Second))
		}
		line += fmt.Sprintf(". Task: %s", truncate(oneLine(a.Task), 200))
		if a.Activity != "" {
			line += fmt.Sprintf(". Right now: %s", a.Activity)
		}
	case AgentIdle:
		if a.Report != "" {
			line += fmt.Sprintf(". Last report: %s", truncate(oneLine(a.Report), 300))
		}
	default:
		if a.Report != "" {
			line += fmt.Sprintf(". %s", truncate(oneLine(a.Report), 300))
		}
	}

	return line
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
