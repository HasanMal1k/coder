package assistant

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/HasanMal1k/coder/internal/instance"
)

// Statuses of a chat's architecture.
const (
	// DesignProposed: put to the user, who has not agreed to it yet.
	DesignProposed = "proposed"
	// DesignApproved: agreed, and what is being built.
	DesignApproved = "approved"
)

// maxNodes bounds one design. A system with more parts than this is not one
// a diagram helps with.
const maxNodes = 12

// Kinds of component.
const (
	NodeFrontend = "frontend"
	NodeBackend  = "backend"
	NodeService  = "service"
	NodeDatabase = "database"
	NodeCache    = "cache"
	NodeQueue    = "queue"
	NodeCI       = "ci"
	// NodeExternal is something the system uses but that doesn't run on one
	// of the user's machines: a payment provider, a mail service.
	NodeExternal = "external"
)

var nodeKinds = []string{NodeFrontend, NodeBackend, NodeService, NodeDatabase, NodeCache, NodeQueue, NodeCI, NodeExternal}

// How the code that runs is divided, and how its source is kept. They are
// separate questions: several services can live in one repository, and one
// service can be split across several.
var (
	structures   = []string{"single-app", "frontend-and-backend", "microservices"}
	repositories = []string{"one-repository", "repository-per-component"}
)

// ErrNoDesign is returned when a chat has no design waiting for the user's
// approval.
var ErrNoDesign = errors.New("there is no design waiting to be approved")

// Design is the architecture of what a chat is building: its components,
// which machine each runs on, how they connect, and why it is shaped that
// way. It is what the user sees as a diagram and agrees to before anything
// is built.
type Design struct {
	// Summary says in a sentence or two what is being built.
	Summary string `json:"summary"`
	// Structure is how the running code is divided: one of structures.
	Structure string `json:"structure"`
	// Repository is how the source is kept: one of repositories.
	Repository string `json:"repository"`
	// Reasons explain the choices, one each, for the user to read.
	Reasons []string `json:"reasons"`
	Nodes   []Node   `json:"nodes"`
	Edges   []Edge   `json:"edges"`
}

// Node is one component.
type Node struct {
	// ID names the node within the design: short, lowercase.
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Name is what the user sees it called.
	Name string `json:"name"`
	// Tech is what it is made with: "Next.js", "ASP.NET Core", "PostgreSQL".
	Tech string `json:"tech"`
	// Why says, in a sentence, why this component and this technology.
	Why string `json:"why,omitempty"`
	// Machine is the name of the machine it runs on. Components may share
	// one. Empty for an external component.
	Machine string `json:"machine,omitempty"`
	// Stack, Size and DiskGB describe that machine.
	Stack  string `json:"stack,omitempty"`
	Size   string `json:"size,omitempty"`
	DiskGB int    `json:"disk_gb,omitempty"`
	// Port is the port it listens on, if it listens on one.
	Port int `json:"port,omitempty"`
}

// Edge is one component using another.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Label says what passes between them: "REST over HTTP", "SQL".
	Label string `json:"label,omitempty"`
	// Path makes the connection one a browser can use. Requests the user's
	// browser makes to this path on From's address are passed on to To, so
	// a page can call its backend on another machine as if it were its own.
	Path string `json:"path,omitempty"`
	// Env is the name of an environment variable that From's programs are
	// given, holding To's address.
	Env string `json:"env,omitempty"`
}

var (
	validNodeID = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)
	validEnv    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	validPath   = regexp.MustCompile(`^/[A-Za-z0-9._~/-]{1,100}$`)
)

// normalize fills in what was left out and checks the rest. The error is
// worded for the model that wrote the design, which reads it and tries again.
func (d *Design) normalize() error {
	d.Summary = strings.TrimSpace(d.Summary)
	if d.Summary == "" {
		return errors.New("the design needs a summary: a sentence saying what is being built")
	}
	if !slices.Contains(structures, d.Structure) {
		return fmt.Errorf("structure must be one of %s", strings.Join(structures, ", "))
	}
	if !slices.Contains(repositories, d.Repository) {
		return fmt.Errorf("repository must be one of %s", strings.Join(repositories, ", "))
	}
	if len(d.Nodes) == 0 || len(d.Nodes) > maxNodes {
		return fmt.Errorf("a design has between 1 and %d nodes", maxNodes)
	}
	if d.Reasons == nil {
		d.Reasons = []string{}
	}
	if d.Edges == nil {
		d.Edges = []Edge{}
	}

	byID := make(map[string]*Node, len(d.Nodes))
	// What each machine has been said to be, by the first node placed on it.
	machines := make(map[string]*Node)

	for i := range d.Nodes {
		n := &d.Nodes[i]
		n.Name, n.Tech = strings.TrimSpace(n.Name), strings.TrimSpace(n.Tech)

		if !validNodeID.MatchString(n.ID) {
			return fmt.Errorf("node id %q must be 1-32 lowercase letters, digits or hyphens", n.ID)
		}
		if byID[n.ID] != nil {
			return fmt.Errorf("two nodes are called %q; ids must be unique", n.ID)
		}
		byID[n.ID] = n

		if !slices.Contains(nodeKinds, n.Kind) {
			return fmt.Errorf("node %q: kind must be one of %s", n.ID, strings.Join(nodeKinds, ", "))
		}
		if n.Name == "" || n.Tech == "" {
			return fmt.Errorf("node %q needs a name and a tech", n.ID)
		}
		if n.Port < 0 || n.Port > 65535 {
			return fmt.Errorf("node %q: port must be between 1 and 65535, or left out", n.ID)
		}

		if n.Kind == NodeExternal {
			// It runs somewhere else, so there is no machine to describe.
			n.Machine, n.Stack, n.Size, n.DiskGB = "", "", "", 0
			continue
		}

		if !validAgentName.MatchString(n.Machine) {
			return fmt.Errorf("node %q needs a machine: a name of 1-40 lowercase letters, digits or hyphens", n.ID)
		}

		stack, err := instance.StackOf(n.Stack)
		if err != nil {
			return fmt.Errorf("node %q: stack must be one of %s", n.ID, strings.Join(stackNames(), ", "))
		}
		n.Stack = stack.Name

		if n.Size == "" {
			n.Size = "medium"
		}
		size, err := instance.SizeOf(n.Size, n.DiskGB)
		if err != nil {
			return fmt.Errorf("node %q: size must be one of %s, and disk_gb %s", n.ID, strings.Join(powerNames(), ", "), diskChoices())
		}
		n.DiskGB = size.DiskGB

		// Components on one machine describe the same machine.
		if first := machines[n.Machine]; first == nil {
			machines[n.Machine] = n
		} else if first.Stack != n.Stack || first.Size != n.Size || first.DiskGB != n.DiskGB {
			return fmt.Errorf("nodes %q and %q are both on machine %q but give it a different stack, size or disk; a machine has one of each", first.ID, n.ID, n.Machine)
		}
	}

	for i := range d.Edges {
		e := &d.Edges[i]
		from, to := byID[e.From], byID[e.To]

		if from == nil || to == nil {
			return fmt.Errorf("the connection from %q to %q names a node that isn't in the design", e.From, e.To)
		}
		if e.From == e.To {
			return fmt.Errorf("node %q is connected to itself", e.From)
		}
		if e.Env != "" && !validEnv.MatchString(e.Env) {
			return fmt.Errorf("connection %s to %s: env %q must be an environment variable name in capitals, such as API_URL", e.From, e.To, e.Env)
		}
		if e.Path != "" {
			if !validPath.MatchString(e.Path) || strings.HasPrefix(e.Path, "/__coder") {
				return fmt.Errorf("connection %s to %s: path %q must look like /api", e.From, e.To, e.Path)
			}
			if from.Port == 0 || to.Port == 0 || from.Machine == "" || to.Machine == "" {
				return fmt.Errorf("connection %s to %s has a path, which needs both nodes to be on a machine and to have a port", e.From, e.To)
			}
			e.Path = strings.TrimRight(e.Path, "/")
		}
	}

	return nil
}

// Node returns the node with the given ID, or nil.
func (d *Design) Node(id string) *Node {
	for i := range d.Nodes {
		if d.Nodes[i].ID == id {
			return &d.Nodes[i]
		}
	}

	return nil
}

// Machines returns, for each machine the design puts components on, the
// first node that describes it, in the order the machines first appear.
func (d *Design) Machines() []Node {
	var out []Node
	seen := make(map[string]bool)

	for _, n := range d.Nodes {
		if n.Machine != "" && !seen[n.Machine] {
			seen[n.Machine] = true
			out = append(out, n)
		}
	}

	return out
}

// address is where a node is reached from the owner's other machines: a URL
// for something that speaks HTTP, host and port for anything else.
func (n *Node) address() string {
	if n.Machine == "" || n.Port == 0 {
		return ""
	}

	switch n.Kind {
	case NodeFrontend, NodeBackend, NodeService:
		return fmt.Sprintf("http://%s:%d", n.Machine, n.Port)
	default:
		return fmt.Sprintf("%s:%d", n.Machine, n.Port)
	}
}

// Text describes the design in words: for the model, once the user has
// agreed to it, and for the agents that build it.
func (d *Design) Text() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n\nStructure: %s. Source: %s.\n", d.Summary, d.Structure, d.Repository)

	b.WriteString("\nComponents:\n")
	for _, n := range d.Nodes {
		fmt.Fprintf(&b, "- %s (%s, id %q): %s", n.Name, n.Kind, n.ID, n.Tech)
		if n.Machine == "" {
			b.WriteString("; external, not on one of the user's machines")
		} else {
			fmt.Fprintf(&b, "; on machine %q (stack %s, size %s, %d GB disk)", n.Machine, n.Stack, n.Size, n.DiskGB)
		}
		if n.Port != 0 {
			fmt.Fprintf(&b, "; listens on port %d", n.Port)
		}
		b.WriteString("\n")
	}

	if len(d.Edges) > 0 {
		b.WriteString("\nConnections:\n")
		for _, e := range d.Edges {
			from, to := d.Node(e.From), d.Node(e.To)
			fmt.Fprintf(&b, "- %s uses %s", from.Name, to.Name)
			if e.Label != "" {
				fmt.Fprintf(&b, " (%s)", e.Label)
			}
			if addr := to.address(); addr != "" {
				fmt.Fprintf(&b, " at %s", addr)
			}
			if e.Env != "" {
				fmt.Fprintf(&b, "; %s's programs are given that address as %s", from.Name, e.Env)
			}
			if e.Path != "" {
				fmt.Fprintf(&b, "; in the browser, requests to %s on %s are passed on to %s unchanged, so pages call %s and never another machine", e.Path, from.Name, to.Name, e.Path)
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

// Connection is one address a machine's programs are given.
type Connection struct {
	// Env is the environment variable; Value the address in it.
	Env   string `json:"env"`
	Value string `json:"value"`
	// To says what is at that address, in words.
	To string `json:"to"`
}

// connectionsFor returns the addresses the design gives the programs on a
// machine: one for each connection, out of a component on that machine, that
// names a variable.
func (d *Design) connectionsFor(machine string) []Connection {
	var out []Connection

	for _, e := range d.Edges {
		from, to := d.Node(e.From), d.Node(e.To)
		if e.Env == "" || from.Machine != machine {
			continue
		}
		if addr := to.address(); addr != "" {
			out = append(out, Connection{Env: e.Env, Value: addr, To: fmt.Sprintf("%s (%s)", to.Name, to.Tech)})
		}
	}

	return out
}
