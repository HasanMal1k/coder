package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	// maxEventLine caps one line of the agent's event stream. Events carry
	// the whole of every file the agent writes; longer lines are skipped.
	maxEventLine = 1 << 20
	// maxWorkerReply caps the agent's answer to a task.
	maxWorkerReply = 8 << 20
	// maxProgressLength caps one progress note.
	maxProgressLength = 200

	workerHome = "/home/coder/"

	// healthTimeout bounds one check of whether the agent is up. It matters:
	// a request that reaches OpenCode in the moment it starts listening is
	// accepted and then never answered, so a check without a limit can wait
	// for ever on an agent that is in fact ready.
	healthTimeout = 3 * time.Second
	// callTimeout bounds the requests that should be quick: everything
	// except giving the agent a task and following its events.
	callTimeout = 30 * time.Second
)

// errNoSession is returned when the agent has no session with the given ID.
var errNoSession = errors.New("the coding agent has no such session")

// worker talks to the coding agent inside one machine. The agent is OpenCode
// running as a server on the machine's loopback interface; requests reach it
// through the machine's tunnel.
type worker struct {
	client    *http.Client
	transport *http.Transport
}

// newWorker returns a worker whose connections are made by dial.
func newWorker(dial func(ctx context.Context) (net.Conn, error)) *worker {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dial(ctx)
		},
		IdleConnTimeout: 30 * time.Second,
	}

	return &worker{
		client:    &http.Client{Transport: transport},
		transport: transport,
	}
}

// close releases the worker's connections.
func (w *worker) close() {
	w.transport.CloseIdleConnections()
}

// healthy reports whether the agent is up and answering.
func (w *worker) healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()

	return w.do(ctx, http.MethodGet, "/global/health", nil, nil)
}

// createSession starts a new conversation with the agent and returns its ID.
func (w *worker) createSession(ctx context.Context, title string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	var out struct {
		ID string `json:"id"`
	}
	if err := w.do(ctx, http.MethodPost, "/session", map[string]string{"title": title}, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("the coding agent returned a session with no ID")
	}

	return out.ID, nil
}

// hasSession reports whether the agent still has the session.
func (w *worker) hasSession(ctx context.Context, sessionID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	err := w.do(ctx, http.MethodGet, "/session/"+sessionID, nil, nil)
	if errors.Is(err, errNoSession) {
		return false, nil
	}

	return err == nil, err
}

// prompt gives the agent a task in a session and waits, however long it
// takes, for it to finish. It returns what the agent said at the end.
func (w *worker) prompt(ctx context.Context, sessionID, text string) (string, error) {
	in := map[string]any{
		"parts": []map[string]string{{"type": "text", "text": text}},
	}

	var out struct {
		Info struct {
			Error *struct {
				Name string `json:"name"`
				Data struct {
					Message string `json:"message"`
				} `json:"data"`
			} `json:"error"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := w.do(ctx, http.MethodPost, "/session/"+sessionID+"/message", in, &out); err != nil {
		return "", err
	}

	if failure := out.Info.Error; failure != nil {
		detail := failure.Data.Message
		if detail == "" {
			detail = failure.Name
		}
		return "", fmt.Errorf("the coding agent stopped with an error: %s", detail)
	}

	var said []string
	for _, part := range out.Parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			said = append(said, strings.TrimSpace(part.Text))
		}
	}

	return strings.Join(said, "\n\n"), nil
}

// abort stops whatever the agent is doing in a session.
func (w *worker) abort(ctx context.Context, sessionID string) error {
	return w.do(ctx, http.MethodPost, "/session/"+sessionID+"/abort", nil, nil)
}

// watch follows what the agent does in a session, calling progress with a
// line of text for each tool it uses, until ctx is cancelled. It returns once
// it is listening, so nothing that happens afterwards is missed; the returned
// channel is closed when it has stopped and progress will not be called again.
func (w *worker) watch(ctx context.Context, sessionID string, progress func(string)) (<-chan struct{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, workerURL("/event"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("the coding agent's event stream returned %s", resp.Status)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer resp.Body.Close()

		// Each tool use is announced several times as it progresses; it is
		// reported once.
		reported := make(map[string]bool)

		reader := bufio.NewReaderSize(resp.Body, 64*1024)
		for {
			line, err := readLine(reader, maxEventLine)
			if err != nil {
				return
			}

			data, ok := bytes.CutPrefix(line, []byte("data:"))
			if !ok {
				continue
			}
			if note, ok := progressNote(data, sessionID, reported); ok {
				progress(note)
			}
		}
	}()

	return done, nil
}

// progressNote turns one event from the agent into a line of text, if it is
// the first news of a tool being used in the session.
func progressNote(data []byte, sessionID string, reported map[string]bool) (string, bool) {
	var event struct {
		Type       string `json:"type"`
		Properties struct {
			Part struct {
				SessionID string `json:"sessionID"`
				Type      string `json:"type"`
				Tool      string `json:"tool"`
				CallID    string `json:"callID"`
				State     struct {
					Status string                     `json:"status"`
					Input  map[string]json.RawMessage `json:"input"`
				} `json:"state"`
			} `json:"part"`
		} `json:"properties"`
	}
	if json.Unmarshal(data, &event) != nil {
		return "", false
	}

	part := event.Properties.Part
	if event.Type != "message.part.updated" || part.Type != "tool" || part.SessionID != sessionID {
		return "", false
	}

	// "pending" comes first, before the tool's input is known.
	switch part.State.Status {
	case "running", "completed", "error":
	default:
		return "", false
	}
	if reported[part.CallID] {
		return "", false
	}
	reported[part.CallID] = true

	return describe(part.Tool, part.State.Input), true
}

// describe says in a few words what a tool call does.
func describe(tool string, input map[string]json.RawMessage) string {
	arg := func(key string) string {
		var s string
		json.Unmarshal(input[key], &s)
		return s
	}
	path := func(key string) string {
		p := arg(key)
		if p == "" {
			return "a file"
		}
		return strings.TrimPrefix(p, workerHome)
	}

	var note string
	switch tool {
	case "write":
		note = "Writing " + path("filePath")
	case "edit", "multiedit", "patch":
		note = "Editing " + path("filePath")
	case "read":
		note = "Reading " + path("filePath")
	case "bash":
		note = "Running " + strings.Join(strings.Fields(arg("command")), " ")
	case "glob":
		note = "Looking for files matching " + arg("pattern")
	case "grep":
		note = "Searching for " + arg("pattern")
	case "list":
		note = "Listing " + path("path")
	case "webfetch":
		note = "Fetching " + arg("url")
	case "todowrite":
		note = "Updating its plan"
	case "task":
		note = "Handing part of the work to a helper: " + arg("description")

	// The team's tools, through which agents learn about each other.
	case "team_list_machines":
		note = "Looking at the owner's machines"
	case "team_list_agents":
		note = "Checking on the other agents"
	case "team_get_agent":
		note = fmt.Sprintf("Reading up on %s on %s", arg("agent"), arg("machine"))
	case "team_read_notes":
		note = "Reading the team's notes"
	case "team_share_note":
		note = "Leaving a note for the team: " + arg("topic")
	case "team_get_architecture":
		note = "Reading the design"
	case "team_ask_agent":
		note = fmt.Sprintf("Asking %s on %s: %s", arg("agent"), arg("machine"), strings.Join(strings.Fields(arg("question")), " "))
	case "team_run_service":
		note = "Registering service " + arg("name") + ": " + strings.Join(strings.Fields(arg("command")), " ")
	case "team_stop_service":
		note = "Stopping service " + arg("name")
	default:
		note = "Using " + tool
	}

	if runes := []rune(note); len(runes) > maxProgressLength {
		note = string(runes[:maxProgressLength-1]) + "…"
	}

	return note
}

// do makes one request to the agent. A non-nil in is sent as JSON; a non-nil
// out receives the decoded response.
func (w *worker) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, workerURL(path), body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return errNoSession
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return fmt.Errorf("the coding agent returned %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}

	if out == nil {
		return nil
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxWorkerReply))
	if err != nil {
		return fmt.Errorf("read the coding agent's reply: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode the coding agent's reply: %w", err)
	}

	return nil
}

// workerURL is the URL of a path on the agent. The host is never resolved:
// the worker's transport connects through the tunnel whatever it says.
func workerURL(path string) string {
	return "http://agent" + path
}

// readLine reads one line, without its line ending. A line longer than max is
// read to its end and returned empty, so one huge event can't use unbounded
// memory or end the stream.
func readLine(r *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	tooLong := false

	for {
		chunk, err := r.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(chunk) > max {
				tooLong = true
				line = nil
			} else {
				line = append(line, chunk...)
			}
		}

		switch {
		case err == nil:
			return bytes.TrimRight(line, "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			// The line continues past the reader's buffer.
		default:
			return nil, err
		}
	}
}
