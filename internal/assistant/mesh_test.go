package assistant

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/HasanMal1k/coder/internal/linkapi"
	"github.com/HasanMal1k/coder/internal/store"
)

// fakeLinks is the machines with a link attached, and what each was sent.
type fakeLinks struct {
	mu       sync.Mutex
	attached map[string]bool
	sent     map[string][]*linkapi.Frame
}

func newFakeLinks(attached ...string) *fakeLinks {
	f := &fakeLinks{attached: map[string]bool{}, sent: map[string][]*linkapi.Frame{}}
	for _, id := range attached {
		f.attached[id] = true
	}
	return f
}

func (f *fakeLinks) Attached(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attached[id]
}

func (f *fakeLinks) Send(id string, frame *linkapi.Frame) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent[id] = append(f.sent[id], frame)
	return nil
}

// last returns the peers a machine was told most recently, or nil.
func (f *fakeLinks) last(id string) *linkapi.Peers {
	f.mu.Lock()
	defer f.mu.Unlock()
	if frames := f.sent[id]; len(frames) > 0 {
		return frames[len(frames)-1].GetPeers()
	}
	return nil
}

// shop is a design of three components on three machines: web uses api,
// api uses db.
func shop() *Design {
	return &Design{
		Summary: "A shop", Structure: "frontend-and-backend", Repository: "repository-per-component",
		Nodes: []Node{
			{ID: "storefront", Kind: NodeFrontend, Name: "Storefront", Tech: "HTML", Machine: "web", Port: 3000},
			{ID: "api", Kind: NodeBackend, Name: "API", Tech: "Go", Machine: "api", Port: 8000},
			{ID: "database", Kind: NodeDatabase, Name: "Database", Tech: "PostgreSQL", Machine: "db", Port: 5432},
		},
		Edges: []Edge{{From: "storefront", To: "api"}, {From: "api", To: "database"}},
	}
}

func machineNames(mesh *linkapi.Mesh) []string {
	var names []string
	for _, p := range mesh.Peers {
		names = append(names, p.Machine)
	}
	return names
}

func TestPeersAreTheMachinesADesignConnects(t *testing.T) {
	d := shop()

	for machine, want := range map[string][]string{
		"web": {"api"},
		"api": {"web", "db"},
		"db":  {"api"},
		"ci":  nil,
	} {
		if got := d.peersOf(machine); !slices.Equal(got, want) {
			t.Errorf("peers of %s are %v, want %v", machine, got, want)
		}
	}

	// Two components on one machine are not each other's peers.
	d.Nodes[2].Machine = "api"
	if got := d.peersOf("api"); !slices.Equal(got, []string{"web"}) {
		t.Errorf("with the database on the API's machine, its peers are %v", got)
	}
}

func TestMachinesAreToldTheirPeersWhenALinkAttaches(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	chatID := h.chat("alice")

	// web and api exist; db, which the design also names, does not yet.
	h.machines.byOwner["alice"] = []Machine{{ID: "id-web", Name: "web"}, {ID: "id-api", Name: "api"}}
	// Someone else's machine of the same name must never be offered.
	h.machines.byOwner["bob"] = []Machine{{ID: "id-bobs-db", Name: "db"}}

	links := newFakeLinks("id-web", "id-api")
	h.svc.UseLinks(links)
	if err := h.designs.Save(ctx, mustArchitecture(t, chatID, "alice", DesignApproved, shop())); err != nil {
		t.Fatal(err)
	}

	h.svc.LinkAttached(ctx, "id-api")

	api := links.last("id-api")
	if api == nil || len(api.Meshes) != 1 {
		t.Fatalf("api was told %v, want one mesh", api)
	}
	mesh := api.Meshes[0]
	if mesh.Id != chatID || len(mesh.Key) != meshKeyBytes {
		t.Fatalf("the mesh is %q with a key of %d bytes", mesh.Id, len(mesh.Key))
	}
	if got := machineNames(mesh); !slices.Equal(got, []string{"web"}) {
		t.Fatalf("api's peers are %v, want web only: db does not exist yet", got)
	}
	if p := mesh.Peers[0]; p.InstanceId != "id-web" || p.Address != "http://web:7600" {
		t.Fatalf("web is given as %v", p)
	}

	// The machine that was already attached is told as well, with the
	// same key: it is how the two prove themselves to each other.
	web := links.last("id-web")
	if web == nil || len(web.Meshes) != 1 || !slices.Equal(web.Meshes[0].Key, mesh.Key) {
		t.Fatalf("web was told %v", web)
	}
	if got := machineNames(web.Meshes[0]); !slices.Equal(got, []string{"api"}) {
		t.Fatalf("web's peers are %v", got)
	}

	// db is made and attaches: api learns of it.
	h.machines.byOwner["alice"] = append(h.machines.byOwner["alice"], Machine{ID: "id-db", Name: "db"})
	links.attached["id-db"] = true
	h.svc.LinkAttached(ctx, "id-db")

	if got := machineNames(links.last("id-api").Meshes[0]); !slices.Equal(got, []string{"web", "db"}) {
		t.Fatalf("after db attached, api's peers are %v", got)
	}
	if got := links.last("id-db").Meshes[0].Peers[0].InstanceId; got != "id-api" {
		t.Fatalf("db's peer is %s, want alice's api", got)
	}
}

func TestADesignThatIsOnlyProposedMakesNoPeers(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	chatID := h.chat("alice")
	h.machines.byOwner["alice"] = []Machine{{ID: "id-web", Name: "web"}, {ID: "id-api", Name: "api"}}

	links := newFakeLinks("id-web", "id-api")
	h.svc.UseLinks(links)
	if err := h.designs.Save(ctx, mustArchitecture(t, chatID, "alice", DesignProposed, shop())); err != nil {
		t.Fatal(err)
	}

	h.svc.LinkAttached(ctx, "id-web")

	// It is still told, so that what it knew before is replaced: nothing.
	if got := links.last("id-web"); got == nil || len(got.Meshes) != 0 {
		t.Fatalf("web was told %v, want no meshes", got)
	}
}

func TestEachMeshHasItsOwnKeyAndKeepsIt(t *testing.T) {
	h := newHarness(t, nil)

	one, again, other := h.svc.meshKey("chat-1"), h.svc.meshKey("chat-1"), h.svc.meshKey("chat-2")
	if !slices.Equal(one, again) {
		t.Fatal("a mesh's key changed between askings")
	}
	if slices.Equal(one, other) {
		t.Fatal("two meshes share a key")
	}
}

func TestWhatAMachineReportsIsKeptUntilItsLinkGoes(t *testing.T) {
	h := newHarness(t, nil)

	h.svc.LinkStatus("id-api", &linkapi.Status{Linked: []string{"id-web", "id-db"}})
	if got := h.svc.Linked("id-api"); !slices.Equal(got, []string{"id-db", "id-web"}) {
		t.Fatalf("api is linked to %v", got)
	}

	h.svc.LinkDetached("id-api")
	if got := h.svc.Linked("id-api"); len(got) != 0 {
		t.Fatalf("after its link went, api is still linked to %v", got)
	}
}

func TestWithoutLinksNothingIsSent(t *testing.T) {
	h := newHarness(t, nil)
	h.machines.byOwner["alice"] = []Machine{{ID: "id-web", Name: "web"}}

	// Must simply do nothing.
	h.svc.LinkAttached(context.Background(), "id-web")
}

// mustArchitecture is a stored design, as the repository keeps it.
func mustArchitecture(t *testing.T, chatID, ownerID, status string, d *Design) *store.Architecture {
	t.Helper()

	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return &store.Architecture{ChatID: chatID, OwnerID: ownerID, Status: status, Design: raw}
}
