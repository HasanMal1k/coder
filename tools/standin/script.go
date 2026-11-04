package main

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

// A script decides what the stand-in says next. It is given the conversation
// so far, as every request to a model carries it, and works out from that
// how far things have got. Nothing is remembered between requests, so the
// stand-in can be restarted at any moment and several chats can use it at
// once.

// conversation is the messages of one request, and the tools it offers.
type conversation struct {
	messages []message
	tools    []string
	// takes is, for each tool whose schema lists them, the arguments it
	// takes.
	takes map[string][]string
}

func conversationOf(req *request) conversation {
	c := conversation{messages: req.Messages, takes: make(map[string][]string)}
	for _, t := range req.Tools {
		name := t.Function.Name
		c.tools = append(c.tools, name)

		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(t.Function.Parameters, &schema) == nil {
			for property := range schema.Properties {
				c.takes[name] = append(c.takes[name], property)
			}
		}
	}

	return c
}

// call is a reply asking for one tool, by the name it is offered under,
// with only the arguments that tool takes. The coding agent's own tools
// differ between its versions (one wants a description of a command,
// another has no such argument), and a client may refuse an argument it
// does not know.
func (c conversation) call(step, name string, args map[string]any) reply {
	name = c.tool(name)

	if known := c.takes[name]; len(known) > 0 {
		for arg := range args {
			if !slices.Contains(known, arg) {
				delete(args, arg)
			}
		}
	}

	return ask(step, name, args)
}

// tool returns the name of the offered tool called name, with or without a
// prefix: the coding agent offers a tool from its "team" server as
// "team_run_service". It returns name itself if no such tool is offered, so
// that the mistake shows up as the client refusing the call.
func (c conversation) tool(name string) string {
	for _, offered := range c.tools {
		if offered == name {
			return offered
		}
	}
	for _, offered := range c.tools {
		if strings.HasSuffix(offered, "_"+name) {
			return offered
		}
	}

	return name
}

// offers reports whether a tool of that name is on offer.
func (c conversation) offers(name string) bool {
	for _, offered := range c.tools {
		if offered == name {
			return true
		}
	}

	return false
}

// asked reports whether a tool call for the step has been made.
func (c conversation) asked(step string) bool {
	for _, m := range c.messages {
		for _, tc := range m.ToolCalls {
			if stepOf(tc.ID) == step {
				return true
			}
		}
	}

	return false
}

// result returns what the latest tool call for the step came back with, and
// whether it has come back.
func (c conversation) result(step string) (string, bool) {
	var id string
	for _, m := range c.messages {
		for _, tc := range m.ToolCalls {
			if stepOf(tc.ID) == step {
				id = tc.ID
			}
		}
	}
	if id == "" {
		return "", false
	}

	for _, m := range c.messages {
		if m.Role == "tool" && m.ToolCallID == id {
			return string(m.Content), true
		}
	}

	return "", false
}

// last returns the newest message.
func (c conversation) last() message {
	if len(c.messages) == 0 {
		return message{}
	}

	return c.messages[len(c.messages)-1]
}

// from returns the conversation from message i on, with the same tools.
func (c conversation) from(i int) conversation {
	return conversation{messages: c.messages[i:], tools: c.tools, takes: c.takes}
}

// Who is asking.
const (
	// callerAssistant is the platform's assistant, in the control plane.
	callerAssistant = "assistant"
	// callerAgent is a coding agent inside a machine.
	callerAgent = "agent"
	// callerOther is anything else: the coding agent also asks its model
	// for small things, such as a title for a session.
	callerOther = "other"
)

// caller tells the assistant from a coding agent by the tools the request
// offers: only the assistant can give a machine a task, and only a coding
// agent can run a command.
func (c conversation) caller() string {
	switch {
	case c.offers("code_on_machine"):
		return callerAssistant
	case c.offers("bash") || c.offers("write"):
		return callerAgent
	default:
		return callerOther
	}
}

var (
	// The platform puts this in front of every task it gives an agent: the
	// agent's own name, its job, and its machine.
	taskHeader = regexp.MustCompile(`You are ([a-z0-9-]+), the "([a-z0-9-]+)" agent: a coding agent on the machine "([a-z0-9-]+)"`)
	// And this when the owner wrote to the agent in the chat.
	directHeader = regexp.MustCompile(`The owner wrote to you directly, in the project's chat:\n\n`)
	// And this when the task is another agent's question.
	questionHeader = regexp.MustCompile(`Another agent, "([^"]+)" on machine "([^"]+)", is asking you something`)
)

// task is the task a coding agent is on: the newest one in its conversation.
type task struct {
	// Agent, Handle and Machine are who the platform said this agent is:
	// its job, its own name, and where it is.
	Agent, Handle, Machine string
	// Text is the task as given, with the platform's preamble.
	Text string
	// Question is set when the task is another agent's question.
	Question bool
	// Direct is set when the owner wrote to the agent in the chat.
	Direct bool
	// Since is the conversation from this task on.
	Since conversation
}

// currentTask finds the newest task in an agent's conversation. An agent
// keeps one conversation for all its tasks, so earlier ones are in it too.
func (c conversation) currentTask() (task, bool) {
	for i := len(c.messages) - 1; i >= 0; i-- {
		m := c.messages[i]
		if m.Role != "user" {
			continue
		}

		found := taskHeader.FindStringSubmatch(string(m.Content))
		if found == nil {
			continue
		}

		return task{
			Handle:   found[1],
			Agent:    found[2],
			Machine:  found[3],
			Text:     string(m.Content),
			Question: questionHeader.MatchString(string(m.Content)),
			Direct:   directHeader.MatchString(string(m.Content)),
			Since:    c.from(i),
		}, true
	}

	return task{}, false
}
