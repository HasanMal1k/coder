package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HasanMal1k/coder/internal/store"
)

// APIDescriptionPath is where every backend serves the description of its
// API: an OpenAPI document, on the backend's own port. The frontend draws
// that API from it, for the owner to read and try, so it is not optional.
const APIDescriptionPath = "/openapi.json"

// describesAPI reports whether body is an OpenAPI (or Swagger 2) document,
// and how many paths it describes.
func describesAPI(body []byte) (paths int, ok bool) {
	var doc struct {
		OpenAPI string                     `json:"openapi"`
		Swagger string                     `json:"swagger"`
		Paths   map[string]json.RawMessage `json:"paths"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return 0, false
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.") && !strings.HasPrefix(doc.Swagger, "2.") {
		return 0, false
	}

	return len(doc.Paths), true
}

// servesAPI reports whether, by a design the owner approved, what listens on
// a machine's port is a backend: something with an API of its own to
// describe.
func (s *Service) servesAPI(ctx context.Context, machine *store.Instance, port int) bool {
	designs, err := s.approvedDesigns(ctx, machine.OwnerID)
	if err != nil {
		return false
	}

	for _, d := range designs {
		for _, n := range d.design.Nodes {
			if n.Machine == machine.Name && n.Port == port && (n.Kind == NodeBackend || n.Kind == NodeService) {
				return true
			}
		}
	}

	return false
}

// apiDescriptionNote looks at whether a backend that has just come up serves
// the description of its API, and returns what its agent should be told: that
// it is there, or what is wrong and that it must be put right. It is empty
// for anything that is not a backend.
func (s *Service) apiDescriptionNote(ctx context.Context, machine *store.Instance, port int) string {
	if !s.servesAPI(ctx, machine, port) {
		return ""
	}

	const rule = "Every backend must serve an OpenAPI 3 document describing all of its routes at " + APIDescriptionPath + " on its own port: the owner reads and tries the API from it. Add it (use the framework's own generator where it has one), restart the service by registering it again, and check that it answers."

	status, body, err := s.machines.Get(ctx, machine.ID, port, APIDescriptionPath)
	if err != nil {
		return fmt.Sprintf(" But this is a backend, and asking it for %s failed (%v). %s", APIDescriptionPath, err, rule)
	}
	if status != 200 {
		return fmt.Sprintf(" But this is a backend, and GET %s answered %d. %s", APIDescriptionPath, status, rule)
	}

	paths, ok := describesAPI(body)
	switch {
	case !ok:
		return fmt.Sprintf(" But this is a backend, and what it serves at %s is not an OpenAPI document. %s", APIDescriptionPath, rule)
	case paths == 0:
		return fmt.Sprintf(" But the API description at %s lists no paths. It must describe every route the backend serves; fill it in and register the service again.", APIDescriptionPath)
	}

	return fmt.Sprintf(" Its API description is served at %s (%d paths).", APIDescriptionPath, paths)
}
