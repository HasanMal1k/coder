package assistant

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/HasanMal1k/coder/internal/linkapi"
)

// meshKeyBytes is the length of a mesh's key.
const meshKeyBytes = 32

// Links is how the assistant reaches machines' link daemons. *link.Hub
// implements it.
type Links interface {
	Send(instanceID string, frame *linkapi.Frame) error
	Attached(instanceID string) bool
}

// UseLinks gives the assistant the machines' links. Without it nothing is
// told to machines about each other, and everything else works as before.
func (s *Service) UseLinks(links Links) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.links = links
}

// LinkAttached is called when a machine's link daemon has attached. The
// machine is told its peers, and so are its owner's other machines, which
// may have been waiting to learn this one's ID.
func (s *Service) LinkAttached(ctx context.Context, instanceID string) {
	ownerID, _, err := s.machines.Lookup(ctx, instanceID)
	if err != nil {
		log.Printf("mesh: look up machine %s: %v", instanceID, err)
		return
	}

	s.tellPeers(ctx, ownerID)
}

// LinkDetached forgets what a machine had reported of itself.
func (s *Service) LinkDetached(instanceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.linked, instanceID)
}

// LinkStatus records what a machine has just reported of itself.
func (s *Service) LinkStatus(instanceID string, status *linkapi.Status) {
	linked := slices.Clone(status.GetLinked())
	slices.Sort(linked)

	s.mu.Lock()
	changed := !slices.Equal(s.linked[instanceID], linked)
	if s.linked == nil {
		s.linked = make(map[string][]string)
	}
	s.linked[instanceID] = linked
	s.mu.Unlock()

	if changed {
		log.Printf("mesh: machine %s is linked to %d peer(s): %s", instanceID, len(linked), strings.Join(linked, ", "))
	}
}

// Linked returns the IDs of the peers a machine says it has a proven stream
// with right now.
func (s *Service) Linked(instanceID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.linked[instanceID])
}

// tellPeers sends each of an owner's attached machines its peers, as the
// owner's approved designs have them now.
func (s *Service) tellPeers(ctx context.Context, ownerID string) {
	s.mu.Lock()
	links := s.links
	s.mu.Unlock()
	if links == nil {
		return
	}

	designs, err := s.approvedDesigns(ctx, ownerID)
	if err != nil {
		log.Printf("mesh: read designs: %v", err)
		return
	}
	machines, err := s.machines.List(ctx, ownerID)
	if err != nil {
		log.Printf("mesh: list machines: %v", err)
		return
	}
	byName := make(map[string]Machine, len(machines))
	for _, m := range machines {
		byName[m.Name] = m
	}

	for _, m := range machines {
		if !links.Attached(m.ID) {
			continue
		}

		peers := &linkapi.Peers{}
		for _, d := range designs {
			var found []*linkapi.Peer
			for _, name := range d.design.peersOf(m.Name) {
				// A machine the design names but nobody has made yet has
				// no ID to be known by; its peers are told when it attaches.
				if peer, ok := byName[name]; ok {
					found = append(found, &linkapi.Peer{
						InstanceId: peer.ID,
						Machine:    peer.Name,
						Address:    fmt.Sprintf("http://%s:%d", peer.Name, linkapi.PeerPort),
					})
				}
			}
			if len(found) > 0 {
				peers.Meshes = append(peers.Meshes, &linkapi.Mesh{Id: d.chatID, Key: s.meshKey(d.chatID), Peers: found})
			}
		}

		// Sent even when empty: it replaces what the machine was told
		// before, and a design that changed may have taken its peers away.
		if err := links.Send(m.ID, &linkapi.Frame{Body: &linkapi.Frame_Peers{Peers: peers}}); err != nil {
			log.Printf("mesh: tell %s its peers: %v", m.Name, err)
		}
	}
}

// meshKey returns the key of a chat's mesh, making one the first time. Keys
// are held in memory only: after a restart machines are given new ones, and
// prove themselves to each other again.
func (s *Service) meshKey(chatID string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.meshKeys == nil {
		s.meshKeys = make(map[string][]byte)
	}
	key, ok := s.meshKeys[chatID]
	if !ok {
		key = make([]byte, meshKeyBytes)
		rand.Read(key)
		s.meshKeys[chatID] = key
	}

	return key
}

// peersOf returns the names of the machines that the components on a
// machine have a connection with, in either direction, in the order the
// design gives them.
func (d *Design) peersOf(machine string) []string {
	var out []string
	for _, e := range d.Edges {
		from, to := d.Node(e.From), d.Node(e.To)
		if from == nil || to == nil {
			continue
		}

		other := ""
		switch machine {
		case from.Machine:
			other = to.Machine
		case to.Machine:
			other = from.Machine
		}
		if other != "" && other != machine && !slices.Contains(out, other) {
			out = append(out, other)
		}
	}

	return out
}
