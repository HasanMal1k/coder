// Package teamapi is how the coding agents inside machines learn about each
// other. A machine asks the control plane, proving it is that machine, and is
// told about its owner's other machines and agents: what each was asked,
// what it did, what it reported, and the notes agents have left one another.
//
// It is offered two ways. Agents ask questions through MCP tools (mcp.go).
// And each machine keeps a copy on its own disk, which its guest daemon
// fetches from here as a set of files.
package teamapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/HasanMal1k/coder/internal/assistant"
	"github.com/HasanMal1k/coder/internal/guestapi"
	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/store"
)

// Guests identifies machines. *instance.Service implements it.
type Guests interface {
	AuthenticateGuest(ctx context.Context, id, token string) error
	ForMachine(ctx context.Context, id string) (*store.Instance, error)
}

// Teams is where the record of a user's agents is kept. *assistant.Service
// implements it.
type Teams interface {
	Team(ctx context.Context, ownerID string) (*assistant.Team, error)
	ShareNote(ctx context.Context, ownerID, machine, author, topic, body string) error
	// MachineRecord is what a machine keeps on disk and is told to run.
	MachineRecord(ctx context.Context, machine *store.Instance) (*assistant.MachineRecord, error)
	// NoteGuest records that a machine has asked for its record, and which
	// services it said it has started (nil if it did not say).
	NoteGuest(instanceID string, started map[string]int64)
	AskAgent(ctx context.Context, ownerID, fromMachine, fromAgent, toMachine, toAgent, question string) (string, error)
	RunService(ctx context.Context, machine *store.Instance, name, command, directory string, port int) (string, error)
	StopService(ctx context.Context, machine *store.Instance, name string) (string, error)
	// PostToChat writes in the owner's chat for an agent on machine.
	PostToChat(ctx context.Context, machine *store.Instance, post assistant.AgentPost) (string, error)
}

type Handler struct {
	guests Guests
	teams  Teams
}

func New(guests Guests, teams Teams) *Handler {
	return &Handler{
		guests: guests,
		teams:  teams,
	}
}

// Files handles GET /guest/team: the record as it should be on the calling
// machine's disk, and the services the machine is to keep running. The
// response carries an ETag, and a request that names the
// current one in If-None-Match is answered 304 with no body, so a machine
// that asks often costs little when nothing has changed.
func (h *Handler) Files(w http.ResponseWriter, r *http.Request) {
	machine, ok := h.authenticate(w, r)
	if !ok {
		return
	}

	// In asking, a machine says which services it has started. One that says
	// nothing is running a daemon from before it could.
	var started map[string]int64
	if said := r.Header.Get(guestapi.ServicesHeader); said != "" {
		if json.Unmarshal([]byte(said), &started) != nil || started == nil {
			started = map[string]int64{}
		}
	}
	h.teams.NoteGuest(machine.ID, started)

	record, err := h.teams.MachineRecord(r.Context(), machine)
	if err != nil {
		log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	body, err := json.Marshal(record)
	if err != nil {
		log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

// authenticate works out which machine is asking, from
// "Authorization: Bearer <instance id>.<guest token>": the same credential a
// machine presents to the model gateway. On failure it has written the
// response.
func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (*store.Instance, bool) {
	key, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	id, token, _ := strings.Cut(key, ".")

	if err := h.guests.AuthenticateGuest(r.Context(), id, token); err != nil {
		if errors.Is(err, instance.ErrGuestUnauthorized) {
			writeError(w, http.StatusUnauthorized, "invalid machine credentials")
			return nil, false
		}
		log.Printf("%s %s: authenticate machine: %v", r.Method, r.URL.Path, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return nil, false
	}

	machine, err := h.guests.ForMachine(r.Context(), id)
	if err != nil {
		if errors.Is(err, instance.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid machine credentials")
			return nil, false
		}
		log.Printf("%s %s: look up machine: %v", r.Method, r.URL.Path, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return nil, false
	}

	return machine, true
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
