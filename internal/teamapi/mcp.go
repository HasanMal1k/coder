package teamapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/HasanMal1k/coder/internal/assistant"
	"github.com/HasanMal1k/coder/internal/store"
)

// This file is a small MCP server: the Model Context Protocol, which is how
// an AI agent is given tools by something outside it. Here the agent is
// OpenCode inside a machine, and the tools let it ask about the rest of the
// team.
//
// MCP is JSON-RPC 2.0. Over HTTP ("streamable HTTP" in the specification),
// the client POSTs each message and the server may answer with plain JSON,
// which is all that request-and-answer tools need. There are no sessions and
// nothing is pushed to the client, so the optional GET stream is not offered.

// protocolVersions are the revisions of MCP this server speaks, newest
// first. Tools work the same way in all of them.
var protocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// maxMCPBody caps one request.
const maxMCPBody = 1 << 20

// JSON-RPC error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	// ID is absent for a notification, which gets no answer.
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// tool is one thing an agent can ask.
type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func schema(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func text(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

var tools = []tool{
	{
		Name:        "list_machines",
		Description: "List the owner's machines: name, whether each is running, its size, and the address other machines reach it at.",
		InputSchema: schema(map[string]any{}),
	},
	{
		Name:        "list_agents",
		Description: "List the coding agents working for the owner, on every machine: what each is called, whether it is working, what it is doing right now and what its latest task is.",
		InputSchema: schema(map[string]any{
			"machine": text("Only agents on this machine. Leave out for all machines."),
		}),
	},
	{
		Name:        "get_agent",
		Description: "Everything recorded about one agent: each task it was given, what it did step by step, and its reports. Use it to find out what another agent built, where, and on which port.",
		InputSchema: schema(map[string]any{
			"machine": text("The machine the agent is on."),
			"agent":   text("The agent's name."),
		}, "machine", "agent"),
	},
	{
		Name:        "get_architecture",
		Description: "The design the owner agreed to: every component, the machine it runs on, and how the components connect. Build to it.",
		InputSchema: schema(map[string]any{}),
	},
	{
		Name:        "ask_agent",
		Description: "Ask another agent a question and get its answer. If it is free it answers now, from what it built; if it is busy your question is left for it and you are given what is recorded about it. Use it for what only that agent knows; for what it has already written down, get_agent and read_notes are quicker.",
		InputSchema: schema(map[string]any{
			"agent_name": text("Your own name, as given at the top of your task."),
			"machine":    text("The machine the other agent is on."),
			"agent":      text("The other agent's name."),
			"question":   text("What you need to know. Be specific."),
		}, "agent_name", "machine", "agent", "question"),
	},
	{
		Name:        "run_service",
		Description: "Register a program for the platform to keep running on this machine: a web server, an API, a database. It is started within seconds, restarted if it exits, started again whenever the machine starts, and given the addresses of the components this machine is connected to as environment variables. You are told whether it came up. Use this for anything that must keep running; never nohup or &.",
		InputSchema: schema(map[string]any{
			"name":      text("A short name for it: lowercase letters, digits, hyphens. Registering the same name again replaces it."),
			"command":   text("The command that runs it, in the foreground: it must not return while the program runs."),
			"directory": text("The folder to run it in. Defaults to /home/coder."),
			"port":      text("The port it listens on, if it listens on one. It must listen on 0.0.0.0."),
		}, "name", "command"),
	},
	{
		Name:        "stop_service",
		Description: "Stop the platform keeping a program running on this machine, and stop the program.",
		InputSchema: schema(map[string]any{
			"name": text("The service's name."),
		}, "name"),
	},
	{
		Name:        "post_to_chat",
		Description: "Write in the project chat, under your name, for the owner to read: something to show them (a mockup, a screenshot, a page you serve) or to tell them. Files are read from this machine and shown in the chat, images as pictures. Your report already reaches whoever gave you the task, so use this for what the owner should see while you work, not for your report.",
		InputSchema: schema(map[string]any{
			"agent_name": text("Your own name, as given at the top of your task."),
			"text":       text("What to say. Markdown is shown as such."),
			"files":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Paths of files on this machine to put in the chat with it, at most 5, each at most 10 MB. Images (PNG, JPEG, SVG, GIF, WebP) are shown as pictures."},
			"port":       text("The port of a page on this machine for the owner to open, if there is one: they get a button. It must listen on 0.0.0.0."),
			"path":       text("The path of that page, such as /mockups/a.html. Defaults to /."),
			"label":      text("A few words for the button, such as \"Mockup A\"."),
		}, "agent_name", "text"),
	},
	{
		Name:        "ask_owner",
		Description: "Ask the owner, in the project chat, to decide something only they can: which design language to follow, which of two mockups, a trade-off that is theirs. Offer the answers as options; show what you are asking about with files or a page. Their answer does not come back in this call: it comes to you later, from them, as a new message. So if you cannot go on without it, end your task after asking and say in your report what you are waiting for.",
		InputSchema: schema(map[string]any{
			"agent_name": text("Your own name, as given at the top of your task."),
			"question":   text("The question, in a sentence or two."),
			"options":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Two to six answers to choose from, a few words each, your recommendation first. The owner can also write their own."},
			"files":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Paths of files on this machine to show with the question, such as mockups, at most 5."},
			"port":       text("The port of a page on this machine to show with the question, if there is one."),
			"path":       text("The path of that page. Defaults to /."),
			"label":      text("A few words for the button that opens it."),
		}, "agent_name", "question"),
	},
	{
		Name:        "read_notes",
		Description: "Read the notes agents have left for each other, such as the shape of an API or a decision that was made.",
		InputSchema: schema(map[string]any{
			"topic": text("Only notes whose topic contains this. Leave out for all notes."),
		}),
	},
	{
		Name:        "share_note",
		Description: "Leave a note for the owner's other agents: something they will need to work with what you made, such as an API's endpoints, a port, a file's location or a decision. Notes are kept and shown to every agent.",
		InputSchema: schema(map[string]any{
			"agent": text("Your own name, as given at the top of your task."),
			"topic": text("What the note is about, in a few words."),
			"text":  text("The note itself."),
		}, "agent", "topic", "text"),
	},
}

// MCP handles POST /mcp.
func (h *Handler) MCP(w http.ResponseWriter, r *http.Request) {
	machine, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMCPBody))
	if err != nil {
		writeRPC(w, http.StatusBadRequest, rpcResponse{Error: &rpcError{Code: codeParseError, Message: "could not read the request"}})
		return
	}

	// One message, or (in older revisions of the protocol) several at once.
	var requests []rpcRequest
	batch := bytes.HasPrefix(bytes.TrimSpace(body), []byte("["))
	if batch {
		err = json.Unmarshal(body, &requests)
	} else {
		var one rpcRequest
		err = json.Unmarshal(body, &one)
		requests = []rpcRequest{one}
	}
	if err != nil || len(requests) == 0 {
		writeRPC(w, http.StatusBadRequest, rpcResponse{Error: &rpcError{Code: codeParseError, Message: "the request is not valid JSON-RPC"}})
		return
	}

	var responses []rpcResponse
	for _, req := range requests {
		if resp, answer := h.handle(r.Context(), machine, req); answer {
			responses = append(responses, resp)
		}
	}

	// Nothing but notifications: there is nothing to say back.
	if len(responses) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if batch {
		writeRPC(w, http.StatusOK, responses)
	} else {
		writeRPC(w, http.StatusOK, responses[0])
	}
}

// handle answers one message. answer is false for a notification.
func (h *Handler) handle(ctx context.Context, machine *store.Instance, req rpcRequest) (resp rpcResponse, answer bool) {
	if len(req.ID) == 0 {
		return rpcResponse{}, false
	}

	resp = rpcResponse{JSONRPC: "2.0", ID: req.ID}
	fail := func(code int, message string) (rpcResponse, bool) {
		resp.Error = &rpcError{Code: code, Message: message}
		return resp, true
	}

	if req.JSONRPC != "2.0" {
		return fail(codeInvalidRequest, `"jsonrpc" must be "2.0"`)
	}

	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &params)

		// Agree to the client's revision if we speak it; otherwise offer
		// our newest and let the client decide.
		version := protocolVersions[0]
		if slices.Contains(protocolVersions, params.ProtocolVersion) {
			version = params.ProtocolVersion
		}

		resp.Result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "coder-team", "version": "1.0.0"},
			"instructions":    "What the owner's other machines and coding agents are doing and have done, and notes agents leave for each other. The same information is kept as files in ~/.coder/team on this machine.",
		}

	case "ping":
		resp.Result = map[string]any{}

	case "tools/list":
		resp.Result = map[string]any{"tools": tools}

	case "tools/call":
		var params struct {
			Name      string                     `json:"name"`
			Arguments map[string]json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return fail(codeInvalidParams, "the arguments must be an object")
		}
		if !slices.ContainsFunc(tools, func(t tool) bool { return t.Name == params.Name }) {
			return fail(codeInvalidParams, fmt.Sprintf("unknown tool %q", params.Name))
		}

		out, err := h.call(ctx, machine, params.Name, plain(params.Arguments))
		switch {
		case err == nil:
			resp.Result = toolResult(out, false)
		case errors.As(err, new(userError)), errors.As(err, new(assistant.Mistake)):
			// The agent got something wrong. That is for the agent to read
			// and put right, not a failure of the protocol.
			resp.Result = toolResult(err.Error(), true)
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			resp.Result = toolResult("That took too long and was given up.", true)
		default:
			return fail(codeInternalError, "the team's record could not be read")
		}

	default:
		return fail(codeMethodNotFound, fmt.Sprintf("method %q is not supported", req.Method))
	}

	return resp, true
}

// plain turns a tool's arguments into text. Models give a port as a number
// as readily as a string; both are taken.
func plain(arguments map[string]json.RawMessage) map[string]string {
	out := make(map[string]string, len(arguments))
	for key, raw := range arguments {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			out[key] = s
		} else if string(raw) != "null" {
			out[key] = string(raw)
		}
	}

	return out
}

// list reads an argument that is a list. Models give a JSON array, or
// sometimes one string, or several on lines of their own.
func list(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var items []string
	if strings.HasPrefix(raw, "[") && json.Unmarshal([]byte(raw), &items) == nil {
		return items
	}

	for _, line := range strings.Split(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			items = append(items, line)
		}
	}
	return items
}

// userError is a mistake in how a tool was called, worded for the agent.
type userError string

func (e userError) Error() string { return string(e) }

// call carries out one tool for the machine that asked.
func (h *Handler) call(ctx context.Context, machine *store.Instance, name string, args map[string]string) (string, error) {
	if name == "share_note" {
		err := h.teams.ShareNote(ctx, machine.OwnerID, machine.Name, args["agent"], args["topic"], args["text"])
		if errors.Is(err, assistant.ErrInvalidNote) {
			return "", userError("A note needs your name (agent), a topic of at most 120 characters, and a text of at most 8000.")
		}
		if err != nil {
			return "", err
		}
		return "Noted. The owner's other agents can read it with read_notes, and it is in ~/.coder/team/NOTES.md on every machine.", nil
	}

	switch name {
	case "ask_agent":
		return h.teams.AskAgent(ctx, machine.OwnerID, machine.Name, args["agent_name"], args["machine"], args["agent"], args["question"])

	case "run_service":
		port := 0
		if raw := strings.TrimSpace(args["port"]); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				return "", userError("The port must be a number.")
			}
			port = n
		}
		return h.teams.RunService(ctx, machine, args["name"], args["command"], args["directory"], port)

	case "stop_service":
		return h.teams.StopService(ctx, machine, args["name"])

	case "post_to_chat", "ask_owner":
		post := assistant.AgentPost{
			Agent:    args["agent_name"],
			Text:     args["text"],
			Files:    list(args["files"]),
			Question: name == "ask_owner",
			Options:  list(args["options"]),
			Path:     args["path"],
			Label:    args["label"],
		}
		if post.Question {
			post.Text = args["question"]
		}
		if raw := strings.TrimSpace(args["port"]); raw != "" {
			port, err := strconv.Atoi(raw)
			if err != nil {
				return "", userError("The port must be a number.")
			}
			post.Port = port
		}
		return h.teams.PostToChat(ctx, machine, post)
	}

	team, err := h.teams.Team(ctx, machine.OwnerID)
	if err != nil {
		return "", err
	}

	switch name {
	case "get_architecture":
		return team.ArchitectureText() + "\n" + team.ConnectionsText(machine.Name), nil

	case "list_machines":
		return fmt.Sprintf("You are on %q.\n\n%s", machine.Name, team.MachinesText()), nil

	case "list_agents":
		return team.AgentsText(args["machine"]), nil

	case "get_agent":
		agent := team.Agent(args["machine"], args["agent"])
		if agent == nil {
			return "", userError(fmt.Sprintf("There is no agent called %q on %q. The agents there are:\n\n%s", args["agent"], args["machine"], team.AgentsText("")))
		}
		return agent.Detail(), nil

	case "read_notes":
		return team.NotesText(args["topic"]), nil
	}

	return "", fmt.Errorf("tool %q has no implementation", name)
}

func toolResult(text string, isError bool) map[string]any {
	result := map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
	}
	if isError {
		result["isError"] = true
	}

	return result
}

func writeRPC(w http.ResponseWriter, status int, v any) {
	// A response that isn't to any request in particular still needs the
	// envelope.
	if resp, ok := v.(rpcResponse); ok && resp.JSONRPC == "" {
		resp.JSONRPC = "2.0"
		resp.ID = json.RawMessage("null")
		v = resp
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
