package assistant

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HasanMal1k/coder/internal/guestapi"
	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/store"
)

const (
	// readyTimeout is how long a machine gets, after being started, to come
	// online with its coding agent answering.
	readyTimeout = 90 * time.Second
	// agentStartTimeout is how long the agent gets to start answering once
	// the machine itself is online. A machine that is online with no agent
	// after this long was built from an image that has none.
	agentStartTimeout = 45 * time.Second
	readyPoll         = 500 * time.Millisecond
	// taskTimeout bounds one coding task.
	taskTimeout = 30 * time.Minute
	// abortTimeout bounds telling the agent to stop.
	abortTimeout = 10 * time.Second
	// getTimeout bounds asking a program on a machine for a page, and
	// getLimit how much of the answer is read.
	getTimeout = 10 * time.Second
	getLimit   = 4 << 20
)

// taskPreamble is put in front of every task. The agent knows nothing about
// the platform it runs on; this tells it what a task from the assistant can't
// be relied on to repeat every time.
const taskPreamble = `You are %[1]q, a coding agent on the machine %[2]q: Ubuntu Linux, user "coder" with passwordless sudo, home /home/coder. This task comes from an assistant working for the machine's owner. Nobody is watching you work, so never wait for input.

Where you are:
- You are one of a team. ~/.coder/team/ on this machine is kept up to date with everything the team knows; start with TEAM.md. ARCHITECTURE.md is the design the owner agreed to: build to it. CONNECTIONS.md says what this machine's components are connected to and the addresses to use for them.
- The team tools give you the same, as of this moment: list_machines, list_agents, get_agent (everything one agent was asked, did and reported), get_architecture, read_notes.
- To learn something only another agent knows, ask it with ask_agent, giving your own name (%[1]q): it answers from what it built. Before you guess how something another agent made works (its port, its API, where its files are), look it up or ask.
- When you make something another agent will have to work with, leave the details with share_note, giving your own name: an API's endpoints and what they return, a database's name and login.

Rules:
- Work under /home/coder, with a folder of its own for each project. Other agents may be working on this machine at the same time: keep to what your task covers, and do not stop, restart or change programs and files you were not asked to touch.
- Install what you need: apt (with sudo, and -y) or a language's own installer. Python 3 and Node.js are on every machine; this one may have more, which TEAM.md says.
- A program that has to keep running (a server, a database) is registered with the run_service tool: a name, the command, its directory and its port. The platform starts it, restarts it if it exits and whenever the machine starts, gives it the addresses in CONNECTIONS.md as environment variables, and tells you whether it came up. The command must stay in the foreground (no & at the end, no nohup) and listen on 0.0.0.0. Never leave a server running from your own shell: it would be gone the next time the machine stops.
- If what you build is a backend or any service with an HTTP API, it must serve an OpenAPI 3 document describing every one of its routes at GET %[4]s on its own port, whatever path the routes themselves are under. Generate it with the framework where it has a generator (ASP.NET Core, FastAPI, NestJS), write it out otherwise, and keep it true to the code. The owner reads and tries the API from it, and run_service tells you if it is missing: the work is not finished until it is served.
- Keep each project folder a git repository, and commit before you report: "git init" if it is new, then "git add -A" and "git commit -m" with what changed.
- End with a short report: what you made and where, what is running and on which port, and anything that did not work.

The task:

%[3]s`

// Instances is what the fleet needs from the instance service.
// *instance.Service implements it.
type Instances interface {
	List(ctx context.Context, ownerID string) ([]*store.Instance, error)
	Create(ctx context.Context, ownerID, name string, opts instance.Options) (*store.Instance, error)
	Start(ctx context.Context, ownerID, id string) (*store.Instance, error)
	Stop(ctx context.Context, ownerID, id string) (*store.Instance, error)
	DialPort(ctx context.Context, id string, port int) (net.Conn, error)
	ForMachine(ctx context.Context, id string) (*store.Instance, error)
	MachineConnected(id string) (done func())
	UserConnected(userID string) (done func())
}

// Sessions remembers each agent's conversation with the agent program on its
// machine. *store.AgentRepository implements it.
type Sessions interface {
	Session(ctx context.Context, agentID string) (string, error)
	SetSession(ctx context.Context, agentID, sessionID string) error
}

// Fleet is the assistant's hands: it implements Machines on top of the
// instance service and the coding agent inside each machine.
type Fleet struct {
	instances Instances
	sessions  Sessions

	readyTimeout      time.Duration
	agentStartTimeout time.Duration
	taskTimeout       time.Duration

	mu sync.Mutex
	// tasks counts, for each machine, the tasks in progress on it.
	tasks map[string]int
}

func NewFleet(instances Instances, sessions Sessions) *Fleet {
	return &Fleet{
		instances:         instances,
		sessions:          sessions,
		readyTimeout:      readyTimeout,
		agentStartTimeout: agentStartTimeout,
		taskTimeout:       taskTimeout,
		tasks:             make(map[string]int),
	}
}

// TaskActive reports whether a coding task is in progress on the instance.
// The model gateway uses it: a machine may call the model only while the
// assistant has it working on something.
func (f *Fleet) TaskActive(instanceID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.tasks[instanceID] > 0
}

// KeepAwake counts the assistant as the user being present, so that their
// machines aren't stopped for being idle while it works for them.
func (f *Fleet) KeepAwake(ownerID string) (done func()) {
	return f.instances.UserConnected(ownerID)
}

// Lookup returns a machine and who owns it.
func (f *Fleet) Lookup(ctx context.Context, instanceID string) (string, Machine, error) {
	inst, err := f.instances.ForMachine(ctx, instanceID)
	if err != nil {
		return "", Machine{}, err
	}

	return inst.OwnerID, machineOf(inst), nil
}

// PortOpen reports whether something on the machine answers on a port. A
// machine that is stopped, or not connected, answers on none.
func (f *Fleet) PortOpen(ctx context.Context, instanceID string, port int) bool {
	conn, err := f.instances.DialPort(ctx, instanceID, port)
	if err != nil {
		return false
	}
	conn.Close()

	return true
}

// Get asks whatever listens on a machine's port for a path, over HTTP. It
// returns the status and up to getLimit bytes of the body.
func (f *Fleet) Get(ctx context.Context, instanceID string, port int, path string) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, getTimeout)
	defer cancel()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return f.instances.DialPort(ctx, instanceID, port)
		},
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:"+strconv.Itoa(port)+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, getLimit))
	if err != nil {
		return 0, nil, err
	}

	return resp.StatusCode, body, nil
}

func (f *Fleet) List(ctx context.Context, ownerID string) ([]Machine, error) {
	instances, err := f.instances.List(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	machines := make([]Machine, 0, len(instances))
	for _, inst := range instances {
		machines = append(machines, machineOf(inst))
	}

	return machines, nil
}

func (f *Fleet) Find(ctx context.Context, ownerID, name string) (Machine, error) {
	inst, err := f.find(ctx, ownerID, name)
	if err != nil {
		return Machine{}, err
	}

	return machineOf(inst), nil
}

// Create makes a machine, starts it and waits for its coding agent.
func (f *Fleet) Create(ctx context.Context, ownerID string, spec MachineSpec, progress func(string)) (Machine, error) {
	name := spec.Name

	size, err := instance.SizeOf(spec.Size, spec.DiskGB)
	if err != nil {
		return Machine{}, explain(name, err)
	}

	progress("Creating the machine")
	inst, err := f.instances.Create(ctx, ownerID, name, instance.Options{Size: size, Stack: spec.Stack})
	if err != nil {
		return Machine{}, explain(name, err)
	}

	progress("Starting it")
	inst, err = f.instances.Start(ctx, ownerID, inst.ID)
	if err != nil {
		return Machine{}, explain(name, err)
	}

	progress("Waiting for its coding agent")
	w := f.worker(inst.ID)
	defer w.close()

	if err := f.waitReady(ctx, w, inst.Name); err != nil {
		return Machine{}, err
	}

	return machineOf(inst), nil
}

func (f *Fleet) Start(ctx context.Context, ownerID, name string) (Machine, error) {
	inst, err := f.find(ctx, ownerID, name)
	if err != nil {
		return Machine{}, err
	}

	inst, err = f.instances.Start(ctx, ownerID, inst.ID)
	if err != nil {
		return Machine{}, explain(name, err)
	}

	return machineOf(inst), nil
}

func (f *Fleet) Stop(ctx context.Context, ownerID, name string) (Machine, error) {
	inst, err := f.find(ctx, ownerID, name)
	if err != nil {
		return Machine{}, err
	}

	inst, err = f.instances.Stop(ctx, ownerID, inst.ID)
	if err != nil {
		return Machine{}, explain(name, err)
	}

	return machineOf(inst), nil
}

// RunTask gives a task to one of the agents on the machine and waits for its
// report, starting the machine first if it is stopped. Each agent is its own
// conversation with the agent program, so several work on one machine at
// once without seeing each other's tasks.
func (f *Fleet) RunTask(ctx context.Context, ownerID, name string, agent AgentRef, task string, progress func(string)) (string, error) {
	inst, err := f.find(ctx, ownerID, name)
	if err != nil {
		return "", err
	}

	// The machine is in use for as long as this takes, whether or not its
	// owner has the app open.
	done := f.instances.MachineConnected(inst.ID)
	defer done()

	release := f.working(inst.ID)
	defer release()

	ctx, cancel := context.WithTimeout(ctx, f.taskTimeout)
	defer cancel()

	if inst.Status != instance.StatusRunning {
		progress("Starting the machine")
		_, err := f.instances.Start(ctx, ownerID, inst.ID)
		// Busy is another agent starting it at the same moment, and
		// waiting for it to be ready below covers that too.
		if err != nil && !errors.Is(err, instance.ErrBusy) {
			return "", explain(name, err)
		}
	}

	w := f.worker(inst.ID)
	defer w.close()

	if err := f.waitReady(ctx, w, inst.Name); err != nil {
		return "", err
	}

	sessionID, err := f.session(ctx, w, agent)
	if err != nil {
		return "", err
	}

	// Progress is nice to have. If the agent's event stream can't be opened
	// the task still runs, just without a commentary.
	watchCtx, stopWatching := context.WithCancel(ctx)
	watching, err := w.watch(watchCtx, sessionID, progress)
	if err != nil {
		log.Printf("assistant: watch the coding agent on %s (%s): %v", inst.Name, inst.ID, err)
	}

	report, err := w.prompt(ctx, sessionID, fmt.Sprintf(taskPreamble, agent.Name, inst.Name, task, APIDescriptionPath))

	stopWatching()
	if watching != nil {
		// After this, progress is not called again: what the caller records
		// next comes after everything the task reported.
		<-watching
	}

	if err != nil {
		if ctx.Err() != nil {
			// We stopped listening, but the agent would carry on working.
			abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
			defer cancel()
			if abortErr := w.abort(abortCtx, sessionID); abortErr != nil {
				log.Printf("assistant: stop the coding agent on %s (%s): %v", inst.Name, inst.ID, abortErr)
			}

			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", fmt.Errorf("the task on %q took longer than %s and was stopped; what it had done so far is still there", inst.Name, f.taskTimeout)
			}
			return "", ctx.Err()
		}
		return "", err
	}

	return report, nil
}

// session returns the agent's conversation with the agent program, starting
// one if there is none or the program no longer has it.
func (f *Fleet) session(ctx context.Context, w *worker, agent AgentRef) (string, error) {
	sessionID, err := f.sessions.Session(ctx, agent.ID)
	switch {
	case err == nil:
		exists, err := w.hasSession(ctx, sessionID)
		if err != nil {
			return "", err
		}
		if exists {
			return sessionID, nil
		}
	case !errors.Is(err, store.ErrNotFound):
		return "", err
	}

	sessionID, err = w.createSession(ctx, "Agent "+agent.Name)
	if err != nil {
		return "", err
	}
	if err := f.sessions.SetSession(ctx, agent.ID, sessionID); err != nil {
		return "", err
	}

	return sessionID, nil
}

// waitReady waits for the machine to be online with its agent answering.
func (f *Fleet) waitReady(ctx context.Context, w *worker, name string) error {
	deadline := time.Now().Add(f.readyTimeout)
	// onlineSince is when the machine was first seen online without an agent.
	var onlineSince time.Time

	noAgent := fmt.Errorf("machine %q has no coding agent: it was created before the assistant existed. Create a new machine and use that instead", name)

	for {
		// Each check has its own time limit, so one that gets no answer is
		// simply tried again.
		err := w.healthy(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		now := time.Now()

		switch {
		case errors.Is(err, instance.ErrPortUnsupported):
			return noAgent
		case errors.Is(err, instance.ErrPortClosed):
			// The machine is online; the agent isn't listening (yet).
			if onlineSince.IsZero() {
				onlineSince = now
			}
			if now.Sub(onlineSince) > f.agentStartTimeout {
				return noAgent
			}
		}
		if now.After(deadline) {
			return fmt.Errorf("machine %q did not become ready: %v", name, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readyPoll):
		}
	}
}

// worker returns a client for the coding agent on an instance.
func (f *Fleet) worker(instanceID string) *worker {
	return newWorker(func(ctx context.Context) (net.Conn, error) {
		return f.instances.DialPort(ctx, instanceID, guestapi.AgentPort)
	})
}

// working counts a task as in progress on the instance until the returned
// function is called.
func (f *Fleet) working(instanceID string) (release func()) {
	f.mu.Lock()
	f.tasks[instanceID]++
	f.mu.Unlock()

	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.tasks[instanceID]--
		if f.tasks[instanceID] == 0 {
			delete(f.tasks, instanceID)
		}
	}
}

func (f *Fleet) find(ctx context.Context, ownerID, name string) (*store.Instance, error) {
	instances, err := f.instances.List(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	for _, inst := range instances {
		if inst.Name == name {
			return inst, nil
		}
	}

	return nil, fmt.Errorf("the user has no machine called %q; list_machines shows what exists", name)
}

func machineOf(inst *store.Instance) Machine {
	return Machine{
		ID:       inst.ID,
		Name:     inst.Name,
		Status:   inst.Status,
		CPUs:     inst.CPUs,
		MemoryMB: inst.MemoryMB,
		DiskGB:   inst.DiskGB,
		Stack:    inst.Stack,
	}
}

// explain rewrites the instance service's errors as something the model can
// act on.
func explain(name string, err error) error {
	switch {
	case errors.Is(err, instance.ErrNameTaken):
		return fmt.Errorf("the user already has a machine called %q; use that one, or pick another name", name)
	case errors.Is(err, instance.ErrInvalidName):
		return fmt.Errorf("%q is not a valid machine name: use 1-40 lowercase letters, digits or hyphens, starting and ending with a letter or digit", name)
	case errors.Is(err, instance.ErrInvalidSize):
		return fmt.Errorf("machine %q can't be that size: size must be one of %s, and disk_gb %s", name, strings.Join(powerNames(), ", "), diskChoices())
	case errors.Is(err, instance.ErrInvalidStack):
		return fmt.Errorf("machine %q can't be on that stack: stack must be one of %s", name, strings.Join(stackNames(), ", "))
	case errors.Is(err, instance.ErrStackUnavailable):
		return fmt.Errorf("machine %q can't be created on that stack: its image has not been built on this server. Use the base stack and have the agent install what it needs, and tell the user that `make images` builds the others", name)
	case errors.Is(err, instance.ErrBusy):
		return fmt.Errorf("machine %q is in the middle of another operation; try again in a moment", name)
	case errors.Is(err, instance.ErrNotProvisioned):
		return fmt.Errorf("machine %q was never created properly and can't be used; create another one", name)
	default:
		return err
	}
}
