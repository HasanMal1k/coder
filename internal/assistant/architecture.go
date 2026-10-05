package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/HasanMal1k/coder/internal/store"
)

const (
	// routeCacheFor is how long the routes of a machine are remembered. The
	// port proxy asks on every request to a machine's port; the answer only
	// changes when a design does.
	routeCacheFor = 5 * time.Second
	// portCheckTimeout bounds finding out whether a port answers.
	portCheckTimeout = 2 * time.Second
)

// Designs is the persistence designs need. *store.ArchitectureRepository
// implements it.
type Designs interface {
	Save(ctx context.Context, a *store.Architecture) error
	Get(ctx context.Context, chatID string) (*store.Architecture, error)
	ListApproved(ctx context.Context, ownerID string) ([]*store.Architecture, error)
}

// Architecture is a chat's design and how what it describes stands right
// now.
type Architecture struct {
	Status    string      `json:"status"`
	Design    Design      `json:"design"`
	Nodes     []NodeState `json:"nodes"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// NodeState is how one component of a design stands.
type NodeState struct {
	ID string `json:"id"`
	// Machine is the machine the component is on, once it exists.
	Machine *Machine `json:"machine,omitempty"`
	// Listening says whether something answers on the component's port.
	// Absent when that can't be known: no port, or the machine isn't
	// running.
	Listening *bool `json:"listening,omitempty"`
}

// Architecture returns the chat's design, or ErrNoDesign if it has none.
func (s *Service) Architecture(ctx context.Context, ownerID, chatID string) (*Architecture, error) {
	chat, err := s.GetChat(ctx, ownerID, chatID)
	if err != nil {
		return nil, err
	}

	stored, design, err := s.design(ctx, chat.ID)
	if err != nil {
		return nil, err
	}

	machines, err := s.machines.List(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]Machine, len(machines))
	for _, m := range machines {
		byName[m.Name] = m
	}

	arch := &Architecture{
		Status:    stored.Status,
		Design:    *design,
		Nodes:     make([]NodeState, len(design.Nodes)),
		UpdatedAt: stored.UpdatedAt,
	}

	// Ports are checked together: each check is a round trip to a machine.
	var wg sync.WaitGroup
	for i, n := range design.Nodes {
		state := &arch.Nodes[i]
		state.ID = n.ID

		m, exists := byName[n.Machine]
		if !exists {
			continue
		}
		state.Machine = &m

		if n.Port != 0 && m.Status == "running" {
			wg.Go(func() {
				checkCtx, cancel := context.WithTimeout(ctx, portCheckTimeout)
				defer cancel()

				open := s.machines.PortOpen(checkCtx, m.ID, n.Port)
				state.Listening = &open
			})
		}
	}
	wg.Wait()

	return arch, nil
}

// design loads and decodes a chat's stored design.
func (s *Service) design(ctx context.Context, chatID string) (*store.Architecture, *Design, error) {
	stored, err := s.designs.Get(ctx, chatID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, ErrNoDesign
		}
		return nil, nil, err
	}

	var design Design
	if err := json.Unmarshal(stored.Design, &design); err != nil {
		return nil, nil, fmt.Errorf("decode design: %w", err)
	}

	return stored, &design, nil
}

// saveDesign stores a design for a chat. Routes are worked out from designs,
// so what is remembered of them is forgotten.
func (s *Service) saveDesign(ctx context.Context, ownerID, chatID, status string, design *Design) error {
	raw, err := json.Marshal(design)
	if err != nil {
		return fmt.Errorf("encode design: %w", err)
	}

	if err := s.designs.Save(ctx, &store.Architecture{ChatID: chatID, OwnerID: ownerID, Status: status, Design: raw}); err != nil {
		return err
	}

	s.mu.Lock()
	clear(s.routes)
	s.mu.Unlock()

	s.notify(chatID)

	return nil
}

// parseDesign reads and checks a design from a tool call's arguments.
func parseDesign(arguments string) (*Design, error) {
	var design Design
	if err := json.Unmarshal([]byte(arguments), &design); err != nil {
		return nil, fmt.Errorf("the design was not valid JSON: %v", err)
	}
	if err := design.normalize(); err != nil {
		return nil, err
	}

	return &design, nil
}

// propose puts a design to the user. The run then waits, as it does for a
// question, until they agree to it or say what to change.
func (s *Service) propose(ctx context.Context, ownerID, chatID string, call ToolCall) error {
	design, err := parseDesign(call.Function.Arguments)
	if err != nil {
		return err
	}

	return s.saveDesign(ctx, ownerID, chatID, DesignProposed, design)
}

// ApproveArchitecture records that the user agrees to the design their chat
// proposed, and sets the assistant to building it. edited, if not nil, is the
// design as the user changed it, and replaces the one proposed.
func (s *Service) ApproveArchitecture(ctx context.Context, ownerID, chatID string, edited *Design) error {
	if s.llm == nil {
		return ErrDisabled
	}

	chat, err := s.GetChat(ctx, ownerID, chatID)
	if err != nil {
		return err
	}
	chatID = chat.ID

	stored, design, err := s.design(ctx, chatID)
	if err != nil {
		return err
	}
	if stored.Status != DesignProposed {
		return ErrNoDesign
	}

	changed := false
	if edited != nil {
		if err := edited.normalize(); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidDesign, err)
		}
		changed = edited.Text() != design.Text()
		design = edited
	}

	// The assistant is waiting on the proposal; nothing else may be the
	// case.
	claimed, err := s.store.TransitionStatus(ctx, chatID, []string{StatusWaiting}, StatusRunning)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrBusy
	}
	release := func() { s.store.SetStatus(context.WithoutCancel(ctx), chatID, StatusWaiting) }

	events, err := s.store.ListEvents(ctx, chatID, 0)
	if err != nil {
		release()
		return err
	}
	pending := unanswered(events, toolProposeArchitecture)
	if len(pending) == 0 {
		release()
		return ErrNoDesign
	}

	if err := s.saveDesign(ctx, ownerID, chatID, DesignApproved, design); err != nil {
		release()
		return err
	}

	verdict := "The user approved the design."
	if changed {
		verdict = "The user approved the design after changing it. Build it as it stands now, not as you proposed it."
	}
	for i, call := range pending {
		content := verdict + " This is what to build:\n\n" + design.Text()
		if i > 0 {
			content = "Replaced by a later proposal, which the user approved."
		}
		if err := s.append(ctx, chatID, KindTool, ToolPayload{ToolCallID: call.ID, Name: toolProposeArchitecture, Content: content}); err != nil {
			release()
			return err
		}
	}
	// Any plain question asked alongside the proposal is answered by it too.
	for _, call := range unanswered(events, toolAskUser) {
		s.append(ctx, chatID, KindTool, ToolPayload{ToolCallID: call.ID, Name: toolAskUser, Content: "(The user answered by approving the design.)"})
	}
	s.notify(chatID)

	s.mu.Lock()
	live := s.liveLocked(chatID)
	live.wakes, live.gaveUp = 0, false
	s.mu.Unlock()

	s.startRun(ownerID, chatID)

	return nil
}

// updateDesign replaces a design the user has already agreed to, for
// corrections that are the assistant's to make: the port something really
// ended up on, a machine's real name.
func (s *Service) updateDesign(ctx context.Context, ownerID, chatID string, call ToolCall) (string, error) {
	stored, _, err := s.design(ctx, chatID)
	if errors.Is(err, ErrNoDesign) || (err == nil && stored.Status != DesignApproved) {
		return "", errors.New("there is no approved design to update; propose one with propose_architecture first")
	}
	if err != nil {
		return "", err
	}

	design, err := parseDesign(call.Function.Arguments)
	if err != nil {
		return "", err
	}
	if err := s.saveDesign(ctx, ownerID, chatID, DesignApproved, design); err != nil {
		return "", err
	}

	return "The design is updated, and the diagram the user sees with it.", nil
}

// ownerDesign is an approved design, with the chat it belongs to.
type ownerDesign struct {
	chatID string
	design Design
}

// approvedDesigns returns the designs the owner has agreed to.
func (s *Service) approvedDesigns(ctx context.Context, ownerID string) ([]ownerDesign, error) {
	stored, err := s.designs.ListApproved(ctx, ownerID)
	if err != nil {
		return nil, err
	}

	out := make([]ownerDesign, 0, len(stored))
	for _, a := range stored {
		var d Design
		if json.Unmarshal(a.Design, &d) != nil {
			continue
		}
		out = append(out, ownerDesign{chatID: a.ChatID, design: d})
	}

	return out, nil
}

// route is one path on a machine's port that leads to another machine.
type route struct {
	port       int
	prefix     string
	targetID   string
	targetPort int
}

type routeTable struct {
	routes  []route
	expires time.Time
}

// Route says where a request for a path on a machine's port should go, if
// not to that port itself. A design's connections can give a path on one
// component's address to another component: a page served from the frontend
// calls /api, and the request reaches the backend on its own machine.
//
// Both machines belong to the same user: the routes of a machine are worked
// out from its owner's designs and its owner's machines only.
func (s *Service) Route(ctx context.Context, instanceID string, port int, path string) (targetID string, targetPort int, ok bool) {
	for _, r := range s.routesFor(ctx, instanceID) {
		if r.port == port && (path == r.prefix || strings.HasPrefix(path, r.prefix+"/")) {
			return r.targetID, r.targetPort, true
		}
	}

	return "", 0, false
}

func (s *Service) routesFor(ctx context.Context, instanceID string) []route {
	s.mu.Lock()
	table, ok := s.routes[instanceID]
	s.mu.Unlock()
	if ok && time.Now().Before(table.expires) {
		return table.routes
	}

	routes := s.buildRoutes(ctx, instanceID)

	s.mu.Lock()
	s.routes[instanceID] = routeTable{routes: routes, expires: time.Now().Add(routeCacheFor)}
	s.mu.Unlock()

	return routes
}

func (s *Service) buildRoutes(ctx context.Context, instanceID string) []route {
	ownerID, self, err := s.machines.Lookup(ctx, instanceID)
	if err != nil {
		return nil
	}
	designs, err := s.approvedDesigns(ctx, ownerID)
	if err != nil || len(designs) == 0 {
		return nil
	}
	machines, err := s.machines.List(ctx, ownerID)
	if err != nil {
		return nil
	}
	ids := make(map[string]string, len(machines))
	for _, m := range machines {
		ids[m.Name] = m.ID
	}

	var routes []route
	for _, d := range designs {
		for _, e := range d.design.Edges {
			from, to := d.design.Node(e.From), d.design.Node(e.To)
			if e.Path == "" || from == nil || to == nil || from.Machine != self.Name {
				continue
			}
			if target, exists := ids[to.Machine]; exists {
				routes = append(routes, route{port: from.Port, prefix: e.Path, targetID: target, targetPort: to.Port})
			}
		}
	}

	// The longest path wins: /api/admin before /api.
	sort.SliceStable(routes, func(i, j int) bool { return len(routes[i].prefix) > len(routes[j].prefix) })

	return routes
}
