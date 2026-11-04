package assistant

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/HasanMal1k/coder/internal/store"
)

// A chat is a room the owner shares with the assistant and the agents the
// assistant has set to work. Each agent has a name of its own ("tom"). The
// owner writes to the assistant by default, or to agents by name ("@tom"),
// in which case the message goes to those agents as a task and the
// assistant only reads it. Agents write in the chat too: their answers to
// the owner, and things they want to show or ask (post_to_chat and
// ask_owner, in teamapi).

const (
	// maxPost is the longest thing an agent can write in the chat.
	maxPost = 8000
	// maxOptions is how many answers an agent's question can offer.
	maxOptions = 6
	maxOption  = 200
	// Agents writing in the chat is bounded like their asking each other:
	// postLimit posts within postWindow, for one owner.
	postLimit  = 40
	postWindow = 10 * time.Minute
)

// ErrUnknownRecipient is returned for a message addressed to a name that is
// not one of the chat's agents.
var ErrUnknownRecipient = errors.New("no agent in this chat has that name")

// UserMessage is what the owner sends in a chat.
type UserMessage struct {
	Text string
	// To names who it is for: agents' handles, or "assistant". Empty means
	// whoever the text mentions with an @, or the assistant if nobody.
	To []string
	// Attachments are the IDs of files uploaded to the chat beforehand.
	Attachments []string
}

// PostPayload is the payload of a "post" event: an agent wrote in the chat.
type PostPayload struct {
	AgentID string `json:"agent_id"`
	// Agent is its handle, and Role its job.
	Agent   string  `json:"agent"`
	Role    string  `json:"role"`
	Machine Machine `json:"machine"`
	Text    string  `json:"text"`
	// Attachments are files it put with what it wrote.
	Attachments []AttachmentRef `json:"attachments,omitempty"`
	// ReplyTo is the owner's message this answers, if it answers one.
	ReplyTo int64 `json:"reply_to,omitempty"`
	// Options make it a question, with these answers to choose from.
	Options []string `json:"options,omitempty"`
	// Question marks a post that asks the owner something.
	Question bool `json:"question,omitempty"`
	// Preview is a page on its machine it wants the owner to open.
	Preview *PostPreview `json:"preview,omitempty"`
	// Queued says the agent was busy when the owner wrote: it gets the
	// message when its task ends. Text is empty.
	Queued bool `json:"queued,omitempty"`
	// Failed and Stopped say the task that should have answered did not
	// finish. Text is why.
	Failed  bool `json:"failed,omitempty"`
	Stopped bool `json:"stopped,omitempty"`
}

// PostPreview is a page an agent serves on its machine.
type PostPreview struct {
	Port  int    `json:"port"`
	Path  string `json:"path,omitempty"`
	Label string `json:"label,omitempty"`
}

// AgentPost is what an agent asks to write in the chat.
type AgentPost struct {
	// Agent is the name the agent gave for itself: its handle or its job.
	Agent string
	Text  string
	// Files are paths on the agent's machine, to put in the chat with it.
	Files []string
	// Options, with Question, offer answers to choose from.
	Question bool
	Options  []string
	// Port, with Path and Label, offers a page the agent serves.
	Port  int
	Path  string
	Label string
}

// SendMessage adds what the owner wrote to the chat, and passes it to whoever
// it is for: the assistant, which is set to work on it as Send does, or
// agents, which are each given it as a task.
func (s *Service) SendMessage(ctx context.Context, ownerID, id string, msg UserMessage) error {
	if s.llm == nil {
		return ErrDisabled
	}

	text := strings.TrimSpace(msg.Text)
	if len(text) > maxMessageLength || (text == "" && len(msg.Attachments) == 0) {
		return ErrInvalidMessage
	}

	chat, err := s.GetChat(ctx, ownerID, id)
	if err != nil {
		return err
	}
	id = chat.ID

	refs, err := s.attachmentRefs(ctx, ownerID, id, msg.Attachments)
	if err != nil {
		return err
	}

	targets, toAssistant, err := s.recipients(ctx, id, msg.To, text)
	if err != nil {
		return err
	}

	payload := UserPayload{Text: text, Attachments: refs}
	if len(targets) > 0 {
		for _, agent := range targets {
			payload.To = append(payload.To, agent.Handle)
		}
		if toAssistant {
			payload.To = append(payload.To, assistantHandle)
		}
	}

	var seq int64
	if toAssistant {
		// Claim the chat. Of two messages sent at once, one wins and the
		// other is told the assistant is busy.
		claimed, err := s.store.TransitionStatus(ctx, id, []string{StatusIdle, StatusWaiting}, StatusRunning)
		if err != nil {
			return err
		}
		if !claimed {
			return ErrBusy
		}

		seq, err = s.recordMessage(ctx, chat, payload)
		if err != nil {
			// Give the chat back, or it would look busy for ever.
			s.store.SetStatus(context.WithoutCancel(ctx), id, chat.Status)
			return err
		}
	} else {
		event, err := s.appendEvent(ctx, id, KindUser, payload)
		if err != nil {
			return err
		}
		seq = event.Seq
	}
	s.notify(id)

	for _, agent := range targets {
		s.direct(ownerID, id, seq, agent, text, msg.Attachments)
	}

	if toAssistant {
		// The user has spoken: whatever follows is at their bidding again.
		s.mu.Lock()
		live := s.liveLocked(id)
		live.wakes, live.gaveUp = 0, false
		s.mu.Unlock()

		s.startRun(ownerID, id)
	}

	return nil
}

// recipients works out who a message is for: the agents named, and whether
// the assistant is among them. Names given in to must all be the chat's;
// mentions in the text that aren't are just words.
func (s *Service) recipients(ctx context.Context, chatID string, to []string, text string) ([]*store.Agent, bool, error) {
	explicit := len(to) > 0
	names := to
	if !explicit {
		names = mentions(text)
	}
	if len(names) == 0 {
		return nil, true, nil
	}

	agents, err := s.agents.ListByChat(ctx, chatID)
	if err != nil {
		return nil, false, err
	}

	var (
		targets     []*store.Agent
		toAssistant bool
		seen        = make(map[string]bool)
	)
	for _, name := range names {
		name = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "@"))
		if seen[name] {
			continue
		}
		seen[name] = true

		if name == assistantHandle {
			toAssistant = true
			continue
		}

		var found *store.Agent
		for _, agent := range agents {
			if agent.Handle == name {
				found = agent
				break
			}
		}
		switch {
		case found != nil:
			targets = append(targets, found)
		case explicit:
			return nil, false, fmt.Errorf("%w: %q", ErrUnknownRecipient, name)
		}
	}

	// Mentions of nobody in particular: it is for the assistant.
	if len(targets) == 0 {
		toAssistant = true
	}

	return targets, toAssistant, nil
}

// directTask is what an agent is given when the owner writes to it.
const directTask = `The owner wrote to you directly, in the project's chat:

%s
%s
Do what they ask, if anything, and answer what they asked. Your report is posted in the chat under your name as your reply, so write it to them: short and plain, saying what you did, what you found, or what you need from them.`

// direct gives the owner's message (event seq) to one agent, in the
// background. If the agent is busy the message waits for it, and is given to
// it when its task ends.
func (s *Service) direct(ownerID, chatID string, seq int64, agent *store.Agent, text string, attachments []string) {
	go func() {
		ctx := context.Background()

		claimed, err := s.agents.Begin(ctx, agent.ID, directSummary(text), true)
		if err != nil {
			s.postFailure(ctx, chatID, seq, agent, err)
			return
		}
		if !claimed {
			// Busy. It gets this when its task ends (followUp).
			err := s.agents.AddMessage(ctx, agent.ID, &store.AgentMessage{
				FromOwner: true, Body: text, Attachments: attachments, ChatSeq: &seq,
			})
			if err != nil {
				s.postFailure(ctx, chatID, seq, agent, err)
				return
			}
			s.append(ctx, chatID, KindPost, s.postFrom(agent, PostPayload{ReplyTo: seq, Queued: true}))
			return
		}

		s.answer(ctx, ownerID, chatID, seq, agent, text, attachments)
	}()
}

// answer has an agent that has already been claimed for it work on what the
// owner wrote, and posts its report in the chat as the reply to event seq.
func (s *Service) answer(ctx context.Context, ownerID, chatID string, seq int64, agent *store.Agent, text string, attachments []string) {
	files, where, err := s.taskFiles(ctx, ownerID, chatID, attachments)
	if err != nil {
		s.agents.Finish(ctx, agent.ID, AgentFailed, err.Error())
		s.postFailure(ctx, chatID, seq, agent, err)
		return
	}
	if where != "" {
		where = "\n" + where
	}

	report, err := s.work(ctx, job{
		ownerID:    ownerID,
		chatID:     chatID,
		callID:     directCallID(seq, agent.Handle),
		machine:    Machine{ID: agent.InstanceID, Name: agent.MachineName},
		agent:      agent,
		task:       fmt.Sprintf(directTask, text, where),
		files:      files,
		background: true,
	})

	post := PostPayload{ReplyTo: seq, Text: truncate(strings.TrimSpace(report), maxPost)}
	if err != nil {
		post.Failed = true
		post.Stopped = errors.Is(err, errAgentStopped) || errors.Is(err, context.Canceled)
		post.Text = err.Error()
	}
	s.append(ctx, chatID, KindPost, s.postFrom(agent, post))

	s.followUp(ownerID, chatID, agent)
}

// followUp gives an agent whose task has just ended what the owner wrote to
// it meanwhile, if anything.
func (s *Service) followUp(ownerID, chatID string, agent *store.Agent) {
	go func() {
		ctx := context.Background()

		waiting, err := s.agents.TakeMessages(ctx, agent.ID, true)
		if err != nil || len(waiting) == 0 {
			return
		}

		var (
			texts       []string
			attachments []string
			seq         int64
		)
		for _, m := range waiting {
			texts = append(texts, m.Body)
			attachments = append(attachments, m.Attachments...)
			if m.ChatSeq != nil {
				seq = *m.ChatSeq
			}
		}
		text := strings.Join(texts, "\n\n")
		if len(texts) > 1 {
			text = "(Several messages, oldest first.)\n\n" + text
		}

		claimed, err := s.agents.Begin(ctx, agent.ID, directSummary(text), true)
		if err != nil || !claimed {
			// Something else got to it first. The messages wait again for
			// that to end.
			for _, m := range waiting {
				s.agents.AddMessage(ctx, agent.ID, m)
			}
			return
		}

		s.answer(ctx, ownerID, chatID, seq, agent, text, attachments)
	}()
}

// directSummary is what an agent's record says it is working on when the
// owner wrote to it: the work's full wording goes to the agent program.
func directSummary(text string) string {
	return "A message from the owner: " + truncate(oneLine(text), 500)
}

// directCallID is what the progress of an agent's answer to the owner's
// message (event seq) is recorded under, as a tool call's is.
func directCallID(seq int64, handle string) string {
	return fmt.Sprintf("direct-%d-%s", seq, handle)
}

func (s *Service) postFailure(ctx context.Context, chatID string, seq int64, agent *store.Agent, err error) {
	s.append(ctx, chatID, KindPost, s.postFrom(agent, PostPayload{ReplyTo: seq, Failed: true, Text: err.Error()}))
}

// postFrom fills in who a post is from.
func (s *Service) postFrom(agent *store.Agent, p PostPayload) PostPayload {
	p.AgentID = agent.ID
	p.Agent = agent.Handle
	p.Role = agent.Name
	p.Machine = Machine{ID: agent.InstanceID, Name: agent.MachineName}
	return p
}

// PostToChat writes in the chat on behalf of an agent on machine: something
// it wants to tell or show the owner, or a question for them. Files are read
// from the machine and put in the chat with it.
func (s *Service) PostToChat(ctx context.Context, machine *store.Instance, post AgentPost) (string, error) {
	text := strings.TrimSpace(post.Text)
	if text == "" || len(text) > maxPost {
		return "", Mistake(fmt.Sprintf("Write something, of at most %d characters.", maxPost))
	}
	if len(post.Files) > maxPostFiles {
		return "", Mistake(fmt.Sprintf("At most %d files go with one message.", maxPostFiles))
	}
	if len(post.Options) > maxOptions {
		return "", Mistake(fmt.Sprintf("Offer at most %d answers.", maxOptions))
	}
	var options []string
	for _, option := range post.Options {
		option = strings.TrimSpace(option)
		if option == "" || len(option) > maxOption {
			return "", Mistake(fmt.Sprintf("Each answer is a few words, at most %d characters.", maxOption))
		}
		options = append(options, option)
	}
	if post.Port != 0 && (post.Port < 1 || post.Port > 65535) {
		return "", Mistake("The port must be between 1 and 65535.")
	}

	agent, err := s.agentOn(ctx, machine, post.Agent)
	if err != nil {
		return "", err
	}

	if !s.allow(machine.OwnerID+"/post", postLimit, postWindow) {
		return "", Mistake(fmt.Sprintf("Agents have written in the chat more than %d times in the last %s. Put what you have to say in your report instead.", postLimit, postWindow))
	}

	payload := PostPayload{Text: text, Question: post.Question, Options: options}
	if post.Port != 0 {
		payload.Preview = &PostPreview{Port: post.Port, Path: previewPath(post.Path), Label: strings.TrimSpace(post.Label)}
	}

	for _, file := range post.Files {
		data, where, err := s.machines.GetFile(ctx, machine.ID, file, MaxAttachmentSize)
		if err != nil {
			return "", Mistake(fmt.Sprintf("Could not attach %s: %v. Nothing was posted.", file, err))
		}
		a, err := s.storeAttachment(ctx, machine.OwnerID, agent.ChatID, path.Base(where), data)
		if errors.Is(err, ErrInvalidAttachment) {
			return "", Mistake(fmt.Sprintf("Could not attach %s: a file must have something in it and be at most %d MB. Nothing was posted.", file, MaxAttachmentSize>>20))
		}
		if err != nil {
			return "", err
		}
		payload.Attachments = append(payload.Attachments, refOf(a))
	}

	if err := s.append(ctx, agent.ChatID, KindPost, s.postFrom(agent, payload)); err != nil {
		return "", err
	}

	if post.Question {
		return fmt.Sprintf("Your question is in the chat, under your name (%s). The owner's answer comes to you as a new message from them, once this task has ended. If you cannot go on without it, end this task now, saying in your report what you are waiting for; otherwise carry on with what you can.", agent.Handle), nil
	}

	return fmt.Sprintf("Posted in the chat, under your name (%s).", agent.Handle), nil
}

// agentOn finds the agent on machine that calls itself name, by its handle
// or its job.
func (s *Service) agentOn(ctx context.Context, machine *store.Instance, name string) (*store.Agent, error) {
	name = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "@"))

	agents, err := s.agents.ListByOwner(ctx, machine.OwnerID)
	if err != nil {
		return nil, err
	}

	// Working ones come first, so of two of the same name in different
	// chats, the one doing something is taken.
	var here []string
	for _, agent := range agents {
		if agent.InstanceID != machine.ID {
			continue
		}
		if agent.Handle == name || agent.Name == name {
			return agent, nil
		}
		here = append(here, fmt.Sprintf("%s (%s)", agent.Handle, agent.Name))
	}

	if len(here) == 0 {
		return nil, Mistake("There are no agents on this machine.")
	}
	return nil, Mistake(fmt.Sprintf("There is no agent called %q on this machine. Give your own name, as at the top of your task; the agents here are: %s.", name, strings.Join(here, ", ")))
}

// previewPath makes a path on a page's address: empty, or starting with /.
func previewPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// allow reports whether something limited to limit times within window, for
// key, may happen now, and counts it if so.
func (s *Service) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	recent := s.asked[key][:0]
	for _, at := range s.asked[key] {
		if now.Sub(at) < window {
			recent = append(recent, at)
		}
	}
	if len(recent) >= limit {
		s.asked[key] = recent
		return false
	}
	s.asked[key] = append(recent, now)

	return true
}
