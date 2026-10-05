package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/HasanMal1k/coder/internal/store"
)

const (
	// teamTasksPerAgent is how many of an agent's latest tasks the team's
	// record holds.
	teamTasksPerAgent = 5
	// teamNotes is how many of the latest notes it holds.
	teamNotes = 50

	maxNoteTopic = 120
	maxNoteBody  = 8000
)

// ErrInvalidNote is returned for a note with nothing in it, or too much.
var ErrInvalidNote = errors.New("invalid note")

// Team is everything one user's agents can know about each other: the
// machines, the agents on them with what each has been asked and has done,
// and the notes agents have left for one another.
type Team struct {
	Machines []TeamMachine `json:"machines"`
	Agents   []TeamAgent   `json:"agents"`
	Notes    []TeamNote    `json:"notes"`
	// Designs are the architectures the owner has agreed to: what the
	// components are and how they connect.
	Designs []TeamDesign `json:"designs"`
	// Services are the programs the platform keeps running.
	Services []TeamService `json:"services"`
}

// TeamDesign is the design of one project.
type TeamDesign struct {
	Project string `json:"project"`
	Design  Design `json:"design"`
}

// TeamService is a program the platform keeps running on a machine.
type TeamService struct {
	Machine   string `json:"machine"`
	Name      string `json:"name"`
	Command   string `json:"command"`
	Directory string `json:"directory"`
	Port      int    `json:"port,omitempty"`
}

type TeamMachine struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	CPUs     int    `json:"cpus"`
	MemoryMB int    `json:"memory_mb"`
	DiskGB   int    `json:"disk_gb"`
	// Stack is the toolchain the machine came with.
	Stack string `json:"stack,omitempty"`
}

type TeamAgent struct {
	Name    string `json:"name"`
	Machine string `json:"machine"`
	// Project is the title of the conversation the agent was started in.
	Project string `json:"project"`
	Status  string `json:"status"`
	// Activity is what it is doing right now, while working.
	Activity string `json:"activity,omitempty"`
	// Tasks are its latest tasks, oldest first.
	Tasks []TeamTask `json:"tasks"`
}

type TeamTask struct {
	Task   string `json:"task"`
	Status string `json:"status"`
	// Log is what the agent did, a line for each step.
	Log        []string   `json:"log"`
	Report     string     `json:"report,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type TeamNote struct {
	Machine   string    `json:"machine"`
	Author    string    `json:"author"`
	Topic     string    `json:"topic"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// Team returns what the user's agents can know about each other.
func (s *Service) Team(ctx context.Context, ownerID string) (*Team, error) {
	machines, err := s.machines.List(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	agents, err := s.agents.ListByOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	tasks, err := s.agents.ListTasks(ctx, ownerID, teamTasksPerAgent)
	if err != nil {
		return nil, err
	}
	notes, err := s.agents.ListNotes(ctx, ownerID, teamNotes)
	if err != nil {
		return nil, err
	}

	designs, err := s.approvedDesigns(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	services, err := s.services.ListByOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	team := &Team{
		Machines: make([]TeamMachine, 0, len(machines)),
		Agents:   make([]TeamAgent, 0, len(agents)),
		Notes:    make([]TeamNote, 0, len(notes)),
		Designs:  make([]TeamDesign, 0, len(designs)),
		Services: make([]TeamService, 0, len(services)),
	}

	names := make(map[string]string, len(machines)) // machine ID -> name
	for _, m := range machines {
		names[m.ID] = m.Name
		team.Machines = append(team.Machines, TeamMachine{
			Name: m.Name, Status: m.Status, CPUs: m.CPUs, MemoryMB: m.MemoryMB, DiskGB: m.DiskGB, Stack: m.Stack,
		})
	}

	for _, d := range designs {
		project := ""
		if chat, err := s.store.GetByID(ctx, d.chatID, ownerID); err == nil {
			project = chat.Title
		}
		team.Designs = append(team.Designs, TeamDesign{Project: project, Design: d.design})
	}

	for _, svc := range services {
		team.Services = append(team.Services, TeamService{
			Machine: names[svc.InstanceID], Name: svc.Name, Command: svc.Command, Directory: svc.Directory, Port: svc.Port,
		})
	}

	byAgent := make(map[string][]TeamTask)
	for _, t := range tasks {
		log := t.Log
		if log == nil {
			log = []string{}
		}
		byAgent[t.AgentID] = append(byAgent[t.AgentID], TeamTask{
			Task: t.Task, Status: t.Status, Log: log, Report: t.Report,
			StartedAt: t.StartedAt, FinishedAt: t.FinishedAt,
		})
	}
	for _, a := range agents {
		agentTasks := byAgent[a.ID]
		if agentTasks == nil {
			agentTasks = []TeamTask{}
		}
		team.Agents = append(team.Agents, TeamAgent{
			Name: a.Name, Machine: a.MachineName, Project: a.ChatTitle,
			Status: a.Status, Activity: a.Activity, Tasks: agentTasks,
		})
	}
	// By machine, then name: a stable order, so what is written to machines
	// only changes when something has really changed.
	sort.SliceStable(team.Agents, func(i, j int) bool {
		a, b := team.Agents[i], team.Agents[j]
		if a.Machine != b.Machine {
			return a.Machine < b.Machine
		}
		return a.Name < b.Name
	})

	for _, n := range notes {
		team.Notes = append(team.Notes, TeamNote{
			Machine: n.Machine, Author: n.Author, Topic: n.Topic, Body: n.Body, CreatedAt: n.CreatedAt,
		})
	}

	return team, nil
}

// ShareNote records something an agent on the named machine wants the
// user's other agents to know.
func (s *Service) ShareNote(ctx context.Context, ownerID, machine, author, topic, body string) error {
	author = strings.TrimSpace(author)
	topic = strings.TrimSpace(topic)
	body = strings.TrimSpace(body)

	if author == "" || topic == "" || body == "" || len(topic) > maxNoteTopic || len(body) > maxNoteBody {
		return ErrInvalidNote
	}

	return s.agents.AddNote(ctx, &store.AgentNote{
		OwnerID: ownerID,
		Machine: machine,
		Author:  author,
		Topic:   topic,
		Body:    body,
	})
}

// Agent returns the named agent on the named machine, or nil.
func (t *Team) Agent(machine, name string) *TeamAgent {
	for i := range t.Agents {
		if t.Agents[i].Machine == machine && t.Agents[i].Name == name {
			return &t.Agents[i]
		}
	}

	return nil
}

// Files renders the team's record as the files kept on a machine, by path
// relative to the folder they live in. self is the machine they are for.
//
// The same record goes to every machine; only the opening lines, which say
// which machine this is, differ.
func (t *Team) Files(self string) map[string]string {
	files := map[string]string{
		"TEAM.md":         t.Overview(self),
		"NOTES.md":        t.NotesText(""),
		"ARCHITECTURE.md": t.ArchitectureText(),
		"CONNECTIONS.md":  t.ConnectionsText(self),
		"connections.env": t.connectionsEnv(self),
	}

	full, _ := json.MarshalIndent(t, "", "  ")
	files["team.json"] = string(full) + "\n"

	for i := range t.Agents {
		a := &t.Agents[i]
		files["agents/"+a.Machine+"--"+a.Name+".md"] = a.Detail()
	}

	return files
}

// Overview is the team at a glance: machines, agents, and where to read more.
func (t *Team) Overview(self string) string {
	var b strings.Builder

	b.WriteString("# The team\n\n")
	if self != "" {
		fmt.Fprintf(&b, "This machine is `%s`. ", self)
	}
	b.WriteString("These are the owner's machines and the coding agents working on them. ")
	b.WriteString("The platform keeps this folder up to date; anything you change here is overwritten.\n\n")

	b.WriteString("## Machines\n\n")
	if len(t.Machines) == 0 {
		b.WriteString("None.\n")
	}
	for _, m := range t.Machines {
		fmt.Fprintf(&b, "- `%s`: %s, %d vCPU, %s memory, %d GB disk. From the owner's other machines it is the hostname `%s`, for example `http://%s:<port>`.\n",
			m.Name, m.Status, m.CPUs, memoryText(m.MemoryMB), m.DiskGB, m.Name, m.Name)
	}

	if len(t.Designs) > 0 {
		b.WriteString("\n## Architecture\n\n")
		b.WriteString("What is being built, the components and how they connect, is in `ARCHITECTURE.md`. ")
		b.WriteString("What this machine's components are connected to, and the addresses to use, is in `CONNECTIONS.md`.\n")
	}

	if len(t.Services) > 0 {
		b.WriteString("\n## Services\n\nPrograms the platform keeps running, and starts again whenever their machine starts.\n\n")
		for _, svc := range t.Services {
			fmt.Fprintf(&b, "- `%s` on `%s`", svc.Name, svc.Machine)
			if svc.Port != 0 {
				fmt.Fprintf(&b, ", port %d", svc.Port)
			}
			fmt.Fprintf(&b, ": `%s` in `%s`\n", svc.Command, svc.Directory)
		}
	}

	b.WriteString("\n## Agents\n\n")
	if len(t.Agents) == 0 {
		b.WriteString("None yet.\n")
	}
	for i := range t.Agents {
		a := &t.Agents[i]
		fmt.Fprintf(&b, "### %s on `%s`\n\n", a.Name, a.Machine)
		fmt.Fprintf(&b, "- Status: %s\n", a.Status)
		if a.Project != "" {
			fmt.Fprintf(&b, "- Project: %s\n", oneLine(a.Project))
		}
		if a.Activity != "" {
			fmt.Fprintf(&b, "- Right now: %s\n", a.Activity)
		}
		if n := len(a.Tasks); n > 0 {
			last := a.Tasks[n-1]
			fmt.Fprintf(&b, "- Latest task (%s): %s\n", last.Status, truncate(oneLine(last.Task), 400))
			if last.Report != "" {
				fmt.Fprintf(&b, "- Its report: %s\n", truncate(oneLine(last.Report), 600))
			}
		}
		fmt.Fprintf(&b, "- Everything it has been asked and has done: `agents/%s--%s.md`\n\n", a.Machine, a.Name)
	}

	b.WriteString("## Notes\n\n")
	if len(t.Notes) == 0 {
		b.WriteString("No agent has left a note yet.\n")
	} else {
		fmt.Fprintf(&b, "Agents have left %d note(s) for each other, in `NOTES.md`. The latest:\n\n", len(t.Notes))
		for _, n := range t.Notes[max(0, len(t.Notes)-5):] {
			fmt.Fprintf(&b, "- %s (from %s on `%s`)\n", n.Topic, n.Author, n.Machine)
		}
	}

	return b.String()
}

// Detail is everything recorded about one agent: each task it was given,
// what it did, and how it ended.
func (a *TeamAgent) Detail() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s on `%s`\n\n", a.Name, a.Machine)
	fmt.Fprintf(&b, "- Status: %s\n", a.Status)
	if a.Project != "" {
		fmt.Fprintf(&b, "- Project: %s\n", oneLine(a.Project))
	}
	if a.Activity != "" {
		fmt.Fprintf(&b, "- Right now: %s\n", a.Activity)
	}

	if len(a.Tasks) == 0 {
		b.WriteString("\nIt has not been given a task yet.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "\nIts latest %d task(s), oldest first.\n", len(a.Tasks))
	for i, task := range a.Tasks {
		fmt.Fprintf(&b, "\n## Task %d (%s)\n\n", i+1, task.Status)
		fmt.Fprintf(&b, "Started %s", task.StartedAt.UTC().Format(time.RFC3339))
		if task.FinishedAt != nil {
			fmt.Fprintf(&b, ", ended %s", task.FinishedAt.UTC().Format(time.RFC3339))
		}
		b.WriteString(".\n\n### What it was asked\n\n")
		b.WriteString(strings.TrimSpace(task.Task) + "\n")

		if len(task.Log) > 0 {
			b.WriteString("\n### What it did\n\n")
			for _, line := range task.Log {
				fmt.Fprintf(&b, "- %s\n", line)
			}
		}
		if task.Report != "" {
			b.WriteString("\n### Its report\n\n")
			b.WriteString(strings.TrimSpace(task.Report) + "\n")
		}
	}

	return b.String()
}

// ArchitectureText is the designs the owner has agreed to, in words.
func (t *Team) ArchitectureText() string {
	if len(t.Designs) == 0 {
		return "# Architecture\n\nNo design has been agreed yet.\n"
	}

	var b strings.Builder
	b.WriteString("# Architecture\n\nWhat the owner has agreed is to be built. Build to this; if it turns out to be wrong, say so in your report and do not quietly do something else.\n")
	for _, d := range t.Designs {
		project := d.Project
		if project == "" {
			project = "A project"
		}
		fmt.Fprintf(&b, "\n## %s\n\n%s", oneLine(project), d.Design.Text())
	}

	return b.String()
}

// connectionsFor returns the addresses the designs give the programs on a
// machine.
func (t *Team) connectionsFor(machine string) []Connection {
	var out []Connection
	for _, d := range t.Designs {
		out = append(out, d.Design.connectionsFor(machine)...)
	}

	return out
}

// ConnectionsText says, for one machine, what its components are connected
// to and how: the addresses to use, the variables they come in, and which
// paths the browser's requests are passed on by.
func (t *Team) ConnectionsText(machine string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Connections of `%s`\n\n", machine)

	outgoing := t.connectionsFor(machine)
	var paths, incoming []string
	for _, d := range t.Designs {
		for _, e := range d.Design.Edges {
			from, to := d.Design.Node(e.From), d.Design.Node(e.To)
			if from.Machine == machine && e.Path != "" {
				paths = append(paths, fmt.Sprintf("- Requests the browser makes to `%s` (and anything under it) on %s, port %d, are passed on to %s at %s, with the path unchanged. Pages must call `%s/...` on their own address, never another machine's.", e.Path, from.Name, from.Port, to.Name, to.address(), e.Path))
			}
			if to.Machine == machine && from.Machine != machine {
				line := fmt.Sprintf("- %s (on `%s`) uses %s here", from.Name, from.Machine, to.Name)
				if e.Path != "" {
					line += fmt.Sprintf("; requests arrive with paths starting `%s`, so serve them under that path", e.Path)
				}
				incoming = append(incoming, line+".")
			}
		}
	}

	if len(outgoing) == 0 && len(paths) == 0 && len(incoming) == 0 {
		b.WriteString("The agreed design connects nothing on this machine to anything else.\n")
		return b.String()
	}

	if len(outgoing) > 0 {
		b.WriteString("## What this machine uses\n\nServices started with `run_service` are given these as environment variables. They are also in `connections.env`, to load by hand (`set -a; . ~/.coder/team/connections.env; set +a`).\n\n")
		for _, c := range outgoing {
			fmt.Fprintf(&b, "- `%s=%s`: %s\n", c.Env, c.Value, c.To)
		}
		b.WriteString("\n")
	}
	if len(paths) > 0 {
		b.WriteString("## From the browser\n\n" + strings.Join(paths, "\n") + "\n\n")
	}
	if len(incoming) > 0 {
		b.WriteString("## What uses this machine\n\n" + strings.Join(incoming, "\n") + "\n")
	}

	return b.String()
}

// connectionsEnv is the same addresses as a file a shell can load.
func (t *Team) connectionsEnv(machine string) string {
	var b strings.Builder
	for _, c := range t.connectionsFor(machine) {
		fmt.Fprintf(&b, "%s=%s\n", c.Env, c.Value)
	}

	return b.String()
}

// NotesText is the notes agents have left, oldest first. With a topic, only
// notes whose topic contains it.
func (t *Team) NotesText(topic string) string {
	var b strings.Builder
	b.WriteString("# Notes from agents\n\n")

	topic = strings.ToLower(strings.TrimSpace(topic))
	shown := 0
	for _, n := range t.Notes {
		if topic != "" && !strings.Contains(strings.ToLower(n.Topic), topic) {
			continue
		}
		shown++
		fmt.Fprintf(&b, "## %s\n\nFrom %s on `%s`, %s.\n\n%s\n\n", n.Topic, n.Author, n.Machine, n.CreatedAt.UTC().Format(time.RFC3339), strings.TrimSpace(n.Body))
	}

	if shown == 0 {
		if topic != "" {
			fmt.Fprintf(&b, "No note has a topic containing %q.\n", topic)
		} else {
			b.WriteString("No agent has left a note yet.\n")
		}
	}

	return b.String()
}

// AgentsText lists the agents, one line each. With a machine, only those on
// it.
func (t *Team) AgentsText(machine string) string {
	var b strings.Builder

	for i := range t.Agents {
		a := &t.Agents[i]
		if machine != "" && a.Machine != machine {
			continue
		}

		fmt.Fprintf(&b, "- %s on %s: %s", a.Name, a.Machine, a.Status)
		if a.Activity != "" {
			fmt.Fprintf(&b, "; right now: %s", a.Activity)
		}
		if n := len(a.Tasks); n > 0 {
			fmt.Fprintf(&b, "; latest task (%s): %s", a.Tasks[n-1].Status, truncate(oneLine(a.Tasks[n-1].Task), 300))
		}
		b.WriteString("\n")
	}

	if b.Len() == 0 {
		if machine != "" {
			return fmt.Sprintf("There are no agents on %q.\n", machine)
		}
		return "There are no agents yet.\n"
	}

	return b.String()
}

// MachinesText lists the machines, one line each.
func (t *Team) MachinesText() string {
	if len(t.Machines) == 0 {
		return "There are no machines.\n"
	}

	var b strings.Builder
	for _, m := range t.Machines {
		fmt.Fprintf(&b, "- %s: %s, %d vCPU, %s memory, %d GB disk; reachable from the owner's other machines as http://%s:<port>",
			m.Name, m.Status, m.CPUs, memoryText(m.MemoryMB), m.DiskGB, m.Name)
		for _, svc := range t.Services {
			if svc.Machine == m.Name && svc.Port != 0 {
				fmt.Fprintf(&b, "; service %q on port %d", svc.Name, svc.Port)
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func memoryText(mb int) string {
	if mb%1024 == 0 {
		return fmt.Sprintf("%d GB", mb/1024)
	}

	return fmt.Sprintf("%d MB", mb)
}
