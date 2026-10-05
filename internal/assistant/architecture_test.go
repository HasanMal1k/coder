package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/HasanMal1k/coder/internal/store"
)

// shopDesign is a React frontend, an ASP.NET backend on its own machine, a
// database and a CI machine: the shape of design the feature exists for.
func shopDesign() map[string]any {
	return map[string]any{
		"summary":    "A shop: a React storefront, an ASP.NET Core API and PostgreSQL.",
		"structure":  "frontend-and-backend",
		"repository": "repository-per-component",
		"reasons":    []string{"Orders are relational and involve money, so PostgreSQL."},
		"nodes": []map[string]any{
			{"id": "frontend", "kind": "frontend", "name": "Storefront", "tech": "React", "machine": "web", "port": 3000},
			{"id": "backend", "kind": "backend", "name": "Orders API", "tech": "ASP.NET Core", "machine": "api", "stack": "dotnet", "size": "large", "port": 8080},
			{"id": "db", "kind": "database", "name": "Database", "tech": "PostgreSQL", "machine": "db", "stack": "data", "disk_gb": 20, "port": 5432},
			{"id": "payments", "kind": "external", "name": "Payments", "tech": "Stripe"},
			{"id": "ci", "kind": "ci", "name": "CI", "tech": "Shell", "machine": "ci"},
		},
		"edges": []map[string]any{
			{"from": "frontend", "to": "backend", "label": "REST", "path": "/api", "env": "API_URL"},
			{"from": "backend", "to": "db", "label": "SQL", "env": "DATABASE_HOST"},
			{"from": "backend", "to": "payments", "label": "HTTPS"},
		},
	}
}

func propose(id string, design map[string]any) Message {
	return wants(callTool(id, toolProposeArchitecture, design))
}

func lastToolResult(h *harness, owner, chatID string) ToolPayload {
	var last ToolPayload
	for _, raw := range h.payloads(owner, chatID, KindTool) {
		json.Unmarshal(raw, &last)
	}
	return last
}

func TestADesignIsProposedChangedAndApproved(t *testing.T) {
	changed := shopDesign()
	changed["summary"] = "A shop, now with MongoDB."

	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		switch call {
		case 0:
			return propose("p1", shopDesign()), nil
		case 1:
			return propose("p2", changed), nil
		default:
			return says("Building it."), nil
		}
	})
	id := h.chat("alice")
	ctx := context.Background()

	h.send("alice", id, "build me a shop")
	if status := h.settle("alice", id); status != StatusWaiting {
		t.Fatalf("status = %q; a proposal waits for the user", status)
	}

	arch, err := h.svc.Architecture(ctx, "alice", id)
	if err != nil || arch.Status != DesignProposed || len(arch.Design.Nodes) != 5 {
		t.Fatalf("Architecture = %+v, %v", arch, err)
	}
	// What was left out is filled in, so the diagram can show it.
	if n := arch.Design.Node("frontend"); n.Stack != "base" || n.Size != "medium" || n.DiskGB != 10 {
		t.Errorf("frontend = %+v, want the defaults filled in", n)
	}
	if n := arch.Design.Node("payments"); n.Machine != "" || n.Size != "" {
		t.Errorf("an external component was given a machine: %+v", n)
	}

	// The user says what to change, in words: not an approval.
	h.send("alice", id, "use MongoDB instead")
	h.settle("alice", id)
	if said := h.llm.sent(1); !strings.Contains(said[len(said)-1].Content, "has not approved") || !strings.Contains(said[len(said)-1].Content, "use MongoDB instead") {
		t.Errorf("the model was told %q", said[len(said)-1].Content)
	}
	if arch, _ := h.svc.Architecture(ctx, "alice", id); arch.Status != DesignProposed || arch.Design.Summary != "A shop, now with MongoDB." {
		t.Fatalf("after asking for a change: %+v", arch)
	}

	if err := h.svc.ApproveArchitecture(ctx, "alice", id, nil); err != nil {
		t.Fatalf("ApproveArchitecture: %v", err)
	}
	if status := h.settle("alice", id); status != StatusIdle {
		t.Errorf("status = %q", status)
	}

	approved := h.llm.sent(2)
	verdict := approved[len(approved)-1]
	if verdict.Role != "tool" || verdict.ToolCallID != "p2" || !strings.Contains(verdict.Content, "The user approved the design.") {
		t.Errorf("the model was told %+v", verdict)
	}
	for _, want := range []string{`on machine "api" (stack dotnet, size large, 10 GB disk)`, "listens on port 8080", "Storefront uses Orders API (REST) at http://api:8080", "given that address as API_URL", "requests to /api on Storefront are passed on to Orders API"} {
		if !strings.Contains(verdict.Content, want) {
			t.Errorf("the approved design, as told to the model, lacks %q:\n%s", want, verdict.Content)
		}
	}
	if arch, _ := h.svc.Architecture(ctx, "alice", id); arch.Status != DesignApproved {
		t.Errorf("status after approval = %q", arch.Status)
	}
}

func TestTheUserCanChangeADesignWhenApprovingIt(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			return propose("p1", shopDesign()), nil
		}
		return says("Building it."), nil
	})
	id := h.chat("alice")
	ctx := context.Background()
	h.send("alice", id, "build me a shop")
	h.settle("alice", id)

	arch, _ := h.svc.Architecture(ctx, "alice", id)

	// Something that doesn't hold together is refused, and nothing changes.
	broken := arch.Design
	broken.Nodes = append([]Node(nil), arch.Design.Nodes...)
	broken.Nodes[0].Size = "gigantic"
	if err := h.svc.ApproveArchitecture(ctx, "alice", id, &broken); !errors.Is(err, ErrInvalidDesign) {
		t.Fatalf("approving a broken design: %v, want ErrInvalidDesign", err)
	}
	if chat, _ := h.svc.GetChat(ctx, "alice", id); chat.Status != StatusWaiting {
		t.Fatalf("status = %q after a refused approval, want still waiting", chat.Status)
	}

	edited := arch.Design
	edited.Nodes = append([]Node(nil), arch.Design.Nodes...)
	edited.Nodes[0].Size = "small"
	if err := h.svc.ApproveArchitecture(ctx, "alice", id, &edited); err != nil {
		t.Fatalf("ApproveArchitecture: %v", err)
	}
	h.settle("alice", id)

	verdict := lastToolResult(h, "alice", id)
	if !strings.Contains(verdict.Content, "after changing it") || !strings.Contains(verdict.Content, `on machine "web" (stack base, size small`) {
		t.Errorf("the model was told %q; it must build what the user changed it to", verdict.Content)
	}
}

func TestMachinesAreCreatedAsTheDesignSays(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		switch call {
		case 0:
			return propose("p1", shopDesign()), nil
		case 1:
			// The model gets the size wrong and forgets the stack.
			return wants(
				callTool("m1", toolCreateMachine, map[string]any{"name": "api", "size": "small"}),
				callTool("m2", toolCreateMachine, map[string]any{"name": "scratch", "size": "small"}),
			), nil
		default:
			return says("Done."), nil
		}
	})
	id := h.chat("alice")
	h.send("alice", id, "build me a shop")
	h.settle("alice", id)
	h.svc.ApproveArchitecture(context.Background(), "alice", id, nil)
	h.settle("alice", id)

	specs := map[string]MachineSpec{}
	for _, spec := range h.machines.specs {
		specs[spec.Name] = spec
	}
	if got := specs["api"]; got != (MachineSpec{Name: "api", Size: "large", DiskGB: 10, Stack: "dotnet"}) {
		t.Errorf("api was created as %+v; want what the user approved, not what the model asked for", got)
	}
	// A machine the design says nothing about is the model's to describe.
	if got := specs["scratch"]; got != (MachineSpec{Name: "scratch", Size: "small"}) {
		t.Errorf("scratch was created as %+v", got)
	}
}

func TestADesignThatDoesNotHoldTogetherGoesBackToTheModel(t *testing.T) {
	bad := shopDesign()
	bad["nodes"].([]map[string]any)[0]["kind"] = "spaceship"

	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			return propose("p1", bad), nil
		}
		return says("Let me fix that."), nil
	})
	id := h.chat("alice")
	h.send("alice", id, "build me a shop")

	if status := h.settle("alice", id); status != StatusIdle {
		t.Errorf("status = %q; nothing valid was proposed, so there is nothing to wait for", status)
	}
	if result := lastToolResult(h, "alice", id); !result.Failed || !strings.Contains(result.Content, "kind must be one of") {
		t.Errorf("result = %+v", result)
	}
	if _, err := h.svc.Architecture(context.Background(), "alice", id); !errors.Is(err, ErrNoDesign) {
		t.Errorf("Architecture: %v, want ErrNoDesign", err)
	}
}

func TestApprovingNeedsSomethingProposedAndTheRightUser(t *testing.T) {
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			return propose("p1", shopDesign()), nil
		}
		return says("ok"), nil
	})
	ctx := context.Background()

	empty := h.chat("alice")
	if err := h.svc.ApproveArchitecture(ctx, "alice", empty, nil); !errors.Is(err, ErrNoDesign) {
		t.Errorf("approving with nothing proposed: %v, want ErrNoDesign", err)
	}

	id := h.chat("alice")
	h.send("alice", id, "build me a shop")
	h.settle("alice", id)

	if err := h.svc.ApproveArchitecture(ctx, "mallory", id, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("approving someone else's design: %v, want ErrNotFound", err)
	}
	if _, err := h.svc.Architecture(ctx, "mallory", id); !errors.Is(err, ErrNotFound) {
		t.Errorf("reading someone else's design: %v, want ErrNotFound", err)
	}

	if err := h.svc.ApproveArchitecture(ctx, "alice", id, nil); err != nil {
		t.Fatalf("ApproveArchitecture: %v", err)
	}
	h.settle("alice", id)
	if err := h.svc.ApproveArchitecture(ctx, "alice", id, nil); !errors.Is(err, ErrNoDesign) {
		t.Errorf("approving twice: %v, want ErrNoDesign", err)
	}
}

func TestUpdatingADesignNeedsOneApproved(t *testing.T) {
	moved := shopDesign()
	moved["nodes"].([]map[string]any)[1]["port"] = 5000

	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		switch call {
		case 0:
			return wants(callTool("u0", toolUpdateArchitecture, shopDesign())), nil
		case 1:
			return propose("p1", shopDesign()), nil
		case 2:
			return wants(callTool("u1", toolUpdateArchitecture, moved)), nil
		default:
			return says("Done."), nil
		}
	})
	id := h.chat("alice")
	ctx := context.Background()

	h.send("alice", id, "build me a shop")
	h.settle("alice", id)
	if first := lastToolResult(h, "alice", id); !first.Failed || !strings.Contains(first.Content, "no approved design") {
		t.Errorf("updating before anything is approved = %+v", first)
	}

	h.svc.ApproveArchitecture(ctx, "alice", id, nil)
	h.settle("alice", id)

	arch, err := h.svc.Architecture(ctx, "alice", id)
	if err != nil || arch.Status != DesignApproved || arch.Design.Node("backend").Port != 5000 {
		t.Errorf("after update_architecture: %+v, %v; want the corrected port, still approved", arch, err)
	}
}

// approved gives alice an approved shop design and the machines it names.
func approved(t *testing.T) (*harness, string) {
	t.Helper()
	h := newHarness(t, func(call int, _ []Message) (Message, error) {
		if call == 0 {
			return propose("p1", shopDesign()), nil
		}
		return says("Built."), nil
	})
	ctx := context.Background()
	for _, name := range []string{"web", "api", "db"} {
		h.machines.Create(ctx, "alice", MachineSpec{Name: name}, func(string) {})
		h.services.owners["id-"+name] = "alice"
	}
	h.machines.Create(ctx, "mallory", MachineSpec{Name: "api"}, func(string) {})

	id := h.chat("alice")
	h.send("alice", id, "build me a shop")
	h.settle("alice", id)
	return h, id
}

func TestAPathOnOneMachineLeadsToAnother(t *testing.T) {
	h, id := approved(t)
	ctx := context.Background()

	// Proposed is not agreed: nothing is routed yet.
	if _, _, ok := h.svc.Route(ctx, "id-web", 3000, "/api/orders"); ok {
		t.Fatal("a design that was only proposed is already routing requests")
	}

	if err := h.svc.ApproveArchitecture(ctx, "alice", id, nil); err != nil {
		t.Fatalf("ApproveArchitecture: %v", err)
	}
	h.settle("alice", id)

	for _, path := range []string{"/api", "/api/orders", "/api/orders/7/items"} {
		target, port, ok := h.svc.Route(ctx, "id-web", 3000, path)
		if !ok || target != "id-api" || port != 8080 {
			t.Errorf("Route(web:3000 %s) = %s:%d, %v; want alice's api machine, port 8080", path, target, port, ok)
		}
	}
	for name, try := range map[string]struct {
		id   string
		port int
		path string
	}{
		"another path":                     {"id-web", 3000, "/"},
		"a path that only starts the same": {"id-web", 3000, "/apiary"},
		"another port":                     {"id-web", 5173, "/api/orders"},
		"the backend itself":               {"id-api", 8080, "/api/orders"},
		"a machine nobody has":             {"id-nothing", 3000, "/api/orders"},
	} {
		if target, _, ok := h.svc.Route(ctx, try.id, try.port, try.path); ok {
			t.Errorf("%s was routed to %s", name, target)
		}
	}
}

func TestTheArchitectureSaysHowThingsStand(t *testing.T) {
	h, id := approved(t)
	ctx := context.Background()
	h.svc.ApproveArchitecture(ctx, "alice", id, nil)
	h.settle("alice", id)

	h.machines.listen("api", 8080, true)

	arch, err := h.svc.Architecture(ctx, "alice", id)
	if err != nil {
		t.Fatalf("Architecture: %v", err)
	}
	states := map[string]NodeState{}
	for _, n := range arch.Nodes {
		states[n.ID] = n
	}

	if s := states["backend"]; s.Machine == nil || s.Machine.ID != "id-api" || s.Listening == nil || !*s.Listening {
		t.Errorf("backend = %+v, want its machine and that it is answering", s)
	}
	if s := states["frontend"]; s.Machine == nil || s.Listening == nil || *s.Listening {
		t.Errorf("frontend = %+v, want its machine and that nothing is answering yet", s)
	}
	if s := states["ci"]; s.Machine != nil || s.Listening != nil {
		t.Errorf("ci = %+v; its machine doesn't exist yet", s)
	}
	if s := states["payments"]; s.Machine != nil {
		t.Errorf("payments = %+v; an external component has no machine", s)
	}
}

func TestConnectionsReachTheMachines(t *testing.T) {
	h, id := approved(t)
	ctx := context.Background()
	h.svc.ApproveArchitecture(ctx, "alice", id, nil)
	h.settle("alice", id)

	web := &store.Instance{ID: "id-web", OwnerID: "alice", Name: "web"}
	api := &store.Instance{ID: "id-api", OwnerID: "alice", Name: "api"}

	h.services.Save(ctx, &store.MachineService{InstanceID: "id-api", Name: "orders", Command: "dotnet run", Directory: "/home/coder/api", Port: 8080})

	record, err := h.svc.MachineRecord(ctx, api)
	if err != nil {
		t.Fatalf("MachineRecord: %v", err)
	}
	if len(record.Services) != 1 || record.Services[0].Env["DATABASE_HOST"] != "db:5432" {
		t.Errorf("the backend's service = %+v; it must be given the database's address", record.Services)
	}
	if got := record.Files["connections.env"]; got != "DATABASE_HOST=db:5432\n" {
		t.Errorf("connections.env on api = %q", got)
	}
	for _, want := range []string{"`DATABASE_HOST=db:5432`: Database (PostgreSQL)", "Storefront (on `web`) uses Orders API here", "requests arrive with paths starting `/api`"} {
		if !strings.Contains(record.Files["CONNECTIONS.md"], want) {
			t.Errorf("CONNECTIONS.md on api lacks %q:\n%s", want, record.Files["CONNECTIONS.md"])
		}
	}
	if !strings.Contains(record.Files["TEAM.md"], "`orders` on `api`, port 8080: `dotnet run`") {
		t.Errorf("TEAM.md does not list the service:\n%s", record.Files["TEAM.md"])
	}

	record, _ = h.svc.MachineRecord(ctx, web)
	for _, want := range []string{"`API_URL=http://api:8080`", "Requests the browser makes to `/api`", "never another machine's"} {
		if !strings.Contains(record.Files["CONNECTIONS.md"], want) {
			t.Errorf("CONNECTIONS.md on web lacks %q:\n%s", want, record.Files["CONNECTIONS.md"])
		}
	}
	if !strings.Contains(record.Files["ARCHITECTURE.md"], "Storefront uses Orders API") {
		t.Errorf("ARCHITECTURE.md = %q", record.Files["ARCHITECTURE.md"])
	}
}

// daemon stands in for a machine's daemon: every few milliseconds it asks
// for its record, saying it has started whatever is registered for it, as a
// real one does within moments of being told. It stops with the test.
func (h *harness) daemon(t *testing.T, instanceID string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() { cancel(); <-done })

	go func() {
		defer close(done)
		for {
			started := map[string]int64{}
			registered, _ := h.services.ListByInstance(ctx, instanceID)
			for _, svc := range registered {
				started[svc.Name] = svc.Revision
			}
			h.svc.NoteGuest(instanceID, started)

			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()
}

func TestServicesAreRegisteredAndWaitedFor(t *testing.T) {
	h, _ := approved(t)
	ctx := context.Background()
	api := &store.Instance{ID: "id-api", OwnerID: "alice", Name: "api"}

	oldTimeout, oldPoll := serviceStartTimeout, servicePoll
	serviceStartTimeout, servicePoll = 300*time.Millisecond, 10*time.Millisecond
	defer func() { serviceStartTimeout, servicePoll = oldTimeout, oldPoll }()
	h.daemon(t, "id-api")

	for name, try := range map[string]struct {
		name, command string
		port          int
	}{
		"a bad name": {"My API", "dotnet run", 8080},
		"no command": {"api", "  ", 8080},
		"a bad port": {"api", "dotnet run", 70000},
	} {
		if _, err := h.svc.RunService(ctx, api, try.name, try.command, "", try.port); !errors.As(err, new(Mistake)) {
			t.Errorf("%s: %v, want it explained to the agent", name, err)
		}
	}

	// It comes up a moment after it is registered, as a real one would.
	go func() {
		time.Sleep(50 * time.Millisecond)
		h.machines.listen("api", 8080, true)
	}()
	said, err := h.svc.RunService(ctx, api, "orders", "dotnet run", "api", 8080)
	if err != nil || !strings.Contains(said, "up and answering on port 8080") {
		t.Errorf("RunService = %q, %v", said, err)
	}
	saved, _ := h.services.ListByInstance(ctx, "id-api")
	if len(saved) != 1 || saved[0].Directory != "/home/coder/api" || saved[0].Command != "dotnet run" {
		t.Errorf("saved = %+v; a relative folder is taken from the home folder", saved[0])
	}

	// One that never comes up is reported, not waited on for ever.
	said, err = h.svc.RunService(ctx, api, "broken", "false", "~/x", 9999)
	if err != nil || !strings.Contains(said, "nothing is answering on port 9999") {
		t.Errorf("a service that never answers = %q, %v", said, err)
	}

	if said, err := h.svc.StopService(ctx, api, "orders"); err != nil || !strings.Contains(said, "stopped") {
		t.Errorf("StopService = %q, %v", said, err)
	}
	if _, err := h.svc.StopService(ctx, api, "orders"); !errors.As(err, new(Mistake)) {
		t.Errorf("stopping it twice: %v", err)
	}
}

func TestDesignsAreChecked(t *testing.T) {
	for name, breakIt := range map[string]func(d map[string]any){
		"no summary":                         func(d map[string]any) { d["summary"] = " " },
		"an unknown structure":               func(d map[string]any) { d["structure"] = "monorepo" },
		"an unknown repository":              func(d map[string]any) { d["repository"] = "microservices" },
		"no nodes":                           func(d map[string]any) { d["nodes"] = []map[string]any{} },
		"two nodes with one id":              func(d map[string]any) { d["nodes"].([]map[string]any)[1]["id"] = "frontend" },
		"a node with no machine":             func(d map[string]any) { delete(d["nodes"].([]map[string]any)[0], "machine") },
		"an unknown stack":                   func(d map[string]any) { d["nodes"].([]map[string]any)[0]["stack"] = "rust" },
		"an unknown disk":                    func(d map[string]any) { d["nodes"].([]map[string]any)[0]["disk_gb"] = 7 },
		"a connection to nothing":            func(d map[string]any) { d["edges"].([]map[string]any)[0]["to"] = "ghost" },
		"a lowercase variable":               func(d map[string]any) { d["edges"].([]map[string]any)[0]["env"] = "api_url" },
		"a path that is not one":             func(d map[string]any) { d["edges"].([]map[string]any)[0]["path"] = "api" },
		"a reserved path":                    func(d map[string]any) { d["edges"].([]map[string]any)[0]["path"] = "/__coder/auth" },
		"a path to a component with no port": func(d map[string]any) { d["edges"].([]map[string]any)[2]["path"] = "/pay" },
		"one machine described two ways": func(d map[string]any) {
			nodes := d["nodes"].([]map[string]any)
			nodes[2]["machine"] = "api" // the database moves in with the backend, but is still on another stack
		},
	} {
		d := shopDesign()
		breakIt(d)
		raw, _ := json.Marshal(d)
		if design, err := parseDesign(string(raw)); err == nil {
			t.Errorf("%s was accepted: %+v", name, design)
		}
	}

	raw, _ := json.Marshal(shopDesign())
	if _, err := parseDesign(string(raw)); err != nil {
		t.Errorf("the design these are all variations of is itself refused: %v", err)
	}
}

func TestABackendMustDescribeItsAPI(t *testing.T) {
	h, id := approved(t)
	ctx := context.Background()
	if err := h.svc.ApproveArchitecture(ctx, "alice", id, nil); err != nil {
		t.Fatalf("ApproveArchitecture: %v", err)
	}
	h.settle("alice", id)
	api := &store.Instance{ID: "id-api", OwnerID: "alice", Name: "api"}
	web := &store.Instance{ID: "id-web", OwnerID: "alice", Name: "web"}

	oldTimeout, oldPoll := serviceStartTimeout, servicePoll
	serviceStartTimeout, servicePoll = 300*time.Millisecond, 10*time.Millisecond
	defer func() { serviceStartTimeout, servicePoll = oldTimeout, oldPoll }()

	h.machines.listen("api", 8080, true)
	h.machines.listen("web", 3000, true)
	h.daemon(t, "id-api")
	h.daemon(t, "id-web")

	// Up, but with no description of its API: its agent is told to add one.
	said, err := h.svc.RunService(ctx, api, "orders", "dotnet run", "api", 8080)
	if err != nil || !strings.Contains(said, "up and answering") || !strings.Contains(said, "GET /openapi.json answered 404") || !strings.Contains(said, "Every backend must serve an OpenAPI 3 document") {
		t.Errorf("a backend with no API description = %q, %v", said, err)
	}

	// Something at that address which is not a description.
	h.machines.serve("api", 8080, "/openapi.json", `{"hello":"world"}`)
	said, _ = h.svc.RunService(ctx, api, "orders", "dotnet run", "api", 8080)
	if !strings.Contains(said, "is not an OpenAPI document") {
		t.Errorf("a backend serving something else there = %q", said)
	}

	// A description that describes nothing.
	h.machines.serve("api", 8080, "/openapi.json", `{"openapi":"3.1.0","info":{"title":"Orders"},"paths":{}}`)
	said, _ = h.svc.RunService(ctx, api, "orders", "dotnet run", "api", 8080)
	if !strings.Contains(said, "lists no paths") {
		t.Errorf("a backend with an empty description = %q", said)
	}

	// Done properly.
	h.machines.serve("api", 8080, "/openapi.json", `{"openapi":"3.0.3","info":{"title":"Orders"},"paths":{"/api/orders":{"get":{}},"/api/orders/{id}":{"get":{}}}}`)
	said, _ = h.svc.RunService(ctx, api, "orders", "dotnet run", "api", 8080)
	if !strings.Contains(said, "API description is served at /openapi.json (2 paths)") || strings.Contains(said, "must serve") {
		t.Errorf("a backend that describes its API = %q", said)
	}

	// A frontend has no API to describe, and is not asked for one. Nor is
	// something else on the backend's machine, on another port.
	said, _ = h.svc.RunService(ctx, web, "web", "npm start", "web", 3000)
	if strings.Contains(said, "openapi") || strings.Contains(said, "API description") {
		t.Errorf("a frontend was asked for an API description: %q", said)
	}
	h.machines.listen("api", 9418, true)
	said, _ = h.svc.RunService(ctx, api, "git", "git daemon", "", 9418)
	if strings.Contains(said, "openapi") || strings.Contains(said, "API description") {
		t.Errorf("a git daemon was asked for an API description: %q", said)
	}
}

func TestWhatCountsAsAnAPIDescription(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		paths int
		ok    bool
	}{
		{"OpenAPI 3", `{"openapi":"3.0.1","paths":{"/a":{},"/b":{}}}`, 2, true},
		{"OpenAPI 3.1", `{"openapi":"3.1.0","paths":{"/a":{}}}`, 1, true},
		{"Swagger 2", `{"swagger":"2.0","paths":{"/a":{}}}`, 1, true},
		{"no paths yet", `{"openapi":"3.0.0","paths":{}}`, 0, true},
		{"some other JSON", `{"paths":{"/a":{}}}`, 0, false},
		{"a web page", `<!doctype html><title>Swagger UI</title>`, 0, false},
		{"nothing", ``, 0, false},
	} {
		paths, ok := describesAPI([]byte(tc.body))
		if paths != tc.paths || ok != tc.ok {
			t.Errorf("%s: got %d, %v; want %d, %v", tc.name, paths, ok, tc.paths, tc.ok)
		}
	}
}

func TestRegisteringAgainWaitsForTheNewProgramNotTheOldOne(t *testing.T) {
	h, _ := approved(t)
	ctx := context.Background()
	api := &store.Instance{ID: "id-api", OwnerID: "alice", Name: "api"}

	oldTimeout, oldPoll := serviceStartTimeout, servicePoll
	serviceStartTimeout, servicePoll = 2*time.Second, 10*time.Millisecond
	defer func() { serviceStartTimeout, servicePoll = oldTimeout, oldPoll }()

	// The program being replaced is still there, answering on the port.
	h.machines.listen("api", 8080, true)

	type result struct {
		said string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		said, err := h.svc.RunService(ctx, api, "orders", "dotnet run", "api", 8080)
		done <- result{said, err}
	}()

	// The machine asks for its record, still running what it had before.
	time.Sleep(50 * time.Millisecond)
	h.svc.NoteGuest("id-api", map[string]int64{"orders": 0})
	select {
	case r := <-done:
		t.Fatalf("it was called up on the strength of the old program answering: %q, %v", r.said, r.err)
	case <-time.After(100 * time.Millisecond):
	}

	// Now it has started the one just registered.
	saved, _ := h.services.ListByInstance(ctx, "id-api")
	h.svc.NoteGuest("id-api", map[string]int64{"orders": saved[0].Revision})
	select {
	case r := <-done:
		if r.err != nil || !strings.Contains(r.said, "up and answering on port 8080") || !strings.Contains(r.said, "started afresh") {
			t.Errorf("RunService = %q, %v", r.said, r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("it never noticed the machine had started the new program")
	}

	// What the machine is told carries the revision, both as itself and in
	// the program's environment, so a daemon of either age restarts it.
	record, err := h.svc.MachineRecord(ctx, api)
	if err != nil || len(record.Services) != 1 {
		t.Fatalf("MachineRecord = %+v, %v", record, err)
	}
	svc := record.Services[0]
	if svc.Revision != saved[0].Revision || svc.Env["CODER_SERVICE_REVISION"] != fmt.Sprint(saved[0].Revision) {
		t.Errorf("service = %+v; want its revision, and that in its environment", svc)
	}

	// A daemon from before machines said what they run: it is given a
	// moment after hearing of the registration, then judged by its port.
	go func() {
		time.Sleep(30 * time.Millisecond)
		h.svc.NoteGuest("id-api", nil)
	}()
	said, err := h.svc.RunService(ctx, api, "orders", "dotnet run", "api", 8080)
	if err != nil || !strings.Contains(said, "up and answering on port 8080") {
		t.Errorf("with an older daemon: %q, %v", said, err)
	}
}
