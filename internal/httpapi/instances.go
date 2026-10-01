package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/HasanMal1k/coder/internal/instance"
	"github.com/HasanMal1k/coder/internal/middleware"
	"github.com/HasanMal1k/coder/internal/portproxy"
	"github.com/HasanMal1k/coder/internal/store"
)

type InstanceHandler struct {
	svc   *instance.Service
	proxy *portproxy.Proxy
}

type CreateInstanceRequest struct {
	Name string `json:"name"`
}

type InstanceResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CPUs      int       `json:"cpus"`
	MemoryMB  int       `json:"memory_mb"`
	DiskGB    int       `json:"disk_gb"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type InstanceListResponse struct {
	Instances []InstanceResponse `json:"instances"`
}

type PortURLRequest struct {
	// Path is where on the port the browser should land. Optional.
	Path string `json:"path"`
}

type PortURLResponse struct {
	URL string `json:"url"`
}

func NewInstanceHandler(svc *instance.Service, proxy *portproxy.Proxy) *InstanceHandler {
	return &InstanceHandler{
		svc:   svc,
		proxy: proxy,
	}
}

func (h *InstanceHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	var req CreateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	inst, err := h.svc.Create(r.Context(), user.ID, req.Name)
	if err != nil {
		writeInstanceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, toInstanceResponse(inst))
}

func (h *InstanceHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	instances, err := h.svc.List(r.Context(), user.ID)
	if err != nil {
		writeInstanceError(w, r, err)
		return
	}

	// Start from an empty slice so a user with no instances gets [] rather
	// than null.
	resp := InstanceListResponse{Instances: []InstanceResponse{}}
	for _, inst := range instances {
		resp.Instances = append(resp.Instances, toInstanceResponse(inst))
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *InstanceHandler) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	inst, err := h.svc.Get(r.Context(), user.ID, chi.URLParam(r, "id"))
	if err != nil {
		writeInstanceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toInstanceResponse(inst))
}

func (h *InstanceHandler) Start(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	inst, err := h.svc.Start(r.Context(), user.ID, chi.URLParam(r, "id"))
	if err != nil {
		writeInstanceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toInstanceResponse(inst))
}

func (h *InstanceHandler) Stop(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	inst, err := h.svc.Stop(r.Context(), user.ID, chi.URLParam(r, "id"))
	if err != nil {
		writeInstanceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toInstanceResponse(inst))
}

func (h *InstanceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	if err := h.svc.Delete(r.Context(), user.ID, chi.URLParam(r, "id")); err != nil {
		writeInstanceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PortURL returns a single-use link that opens one of the instance's ports
// (the editor, or a program the user is running) in the browser.
func (h *InstanceHandler) PortURL(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	port, err := strconv.Atoi(chi.URLParam(r, "port"))
	if err != nil || port < 1 || port > 65535 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "port must be between 1 and 65535"})
		return
	}

	// The body is optional; an empty one means "the root of that port".
	var req PortURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	// This is the ownership check: the link is only issued for an instance
	// this user can see.
	inst, err := h.svc.Get(r.Context(), user.ID, chi.URLParam(r, "id"))
	if err != nil {
		writeInstanceError(w, r, err)
		return
	}

	link, err := h.proxy.URL(user.ID, inst.ID, port, req.Path)
	if err != nil {
		writeInstanceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, PortURLResponse{URL: link})
}

func toInstanceResponse(inst *store.Instance) InstanceResponse {
	return InstanceResponse{
		ID:        inst.ID,
		Name:      inst.Name,
		Status:    inst.Status,
		CPUs:      inst.CPUs,
		MemoryMB:  inst.MemoryMB,
		DiskGB:    inst.DiskGB,
		CreatedAt: inst.CreatedAt,
		UpdatedAt: inst.UpdatedAt,
	}
}

// writeInstanceError maps a service error to a response. Anything unexpected
// is logged and reported as a plain 500, so internals never reach the client.
func writeInstanceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, instance.ErrInvalidName):
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "name must be 1-40 lowercase letters, digits or hyphens, and start and end with a letter or digit"})
	case errors.Is(err, instance.ErrNotFound):
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "instance not found"})
	case errors.Is(err, instance.ErrNameTaken):
		writeJSON(w, http.StatusConflict, ErrorResponse{Error: "you already have an instance with that name"})
	case errors.Is(err, instance.ErrBusy):
		writeJSON(w, http.StatusConflict, ErrorResponse{Error: "instance is busy with another operation"})
	case errors.Is(err, instance.ErrNotProvisioned):
		writeJSON(w, http.StatusConflict, ErrorResponse{Error: "instance failed to create and can only be deleted"})
	default:
		log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
	}
}
