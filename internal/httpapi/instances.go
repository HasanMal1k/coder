package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

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
	// Size is the power level: "small", "medium" or "large". Optional.
	Size string `json:"size"`
	// DiskGB is the size of the home folder's disk. Optional.
	DiskGB int `json:"disk_gb"`
	// Stack is the toolchain to have installed: "base", "go", "cpp",
	// "dotnet" or "data". Optional.
	Stack string `json:"stack"`
}

type InstanceResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CPUs      int       `json:"cpus"`
	MemoryMB  int       `json:"memory_mb"`
	DiskGB    int       `json:"disk_gb"`
	Stack     string    `json:"stack"`
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

	size, err := instance.SizeOf(req.Size, req.DiskGB)
	if err != nil {
		writeInstanceError(w, r, err)
		return
	}

	inst, err := h.svc.Create(r.Context(), user.ID, req.Name, instance.Options{Size: size, Stack: req.Stack})
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

// maxPortCallBody bounds the JSON describing a request to a machine's port,
// the body to send included.
const maxPortCallBody = 1 << 20

type PortCallRequest struct {
	// Method defaults to GET.
	Method string `json:"method"`
	// Path is the path to ask the port for, with any query. Defaults to "/".
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

type PortCallResponse struct {
	// Status is the status the machine answered with.
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	// Base64 says Body is base64, because what came back was not text.
	Base64 bool `json:"base64,omitempty"`
	// Truncated says Body is only the start of what came back.
	Truncated  bool  `json:"truncated,omitempty"`
	DurationMS int64 `json:"duration_ms"`
}

// Request makes an HTTP request to a port inside the caller's machine and
// returns the answer as data. The machine's own status, 404 or 500 included,
// arrives inside a 200: an error status from here means the request could not
// be made at all.
func (h *InstanceHandler) Request(w http.ResponseWriter, r *http.Request) {
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

	// The body is optional; an empty one means "GET the root of that port".
	var req PortCallRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxPortCallBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, ErrorResponse{Error: "the request is larger than 1 MB"})
			return
		}
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	resp, err := h.svc.Request(r.Context(), user.ID, chi.URLParam(r, "id"), port, instance.PortRequest{
		Method:  req.Method,
		Path:    req.Path,
		Headers: req.Headers,
		Body:    []byte(req.Body),
	})
	if err != nil {
		switch {
		case errors.Is(err, instance.ErrInvalidRequest):
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		case errors.Is(err, instance.ErrNotRunning):
			writeJSON(w, http.StatusConflict, ErrorResponse{Error: "instance is not running"})
		case errors.Is(err, instance.ErrGuestOffline):
			writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "instance is not reachable yet; try again in a moment"})
		case errors.Is(err, instance.ErrPortClosed):
			writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "nothing is listening on that port"})
		case errors.Is(err, instance.ErrPortUnsupported):
			writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "this instance was made before ports could be reached; recreate it"})
		case errors.Is(err, instance.ErrBadResponse):
			writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: "what is listening on that port did not answer with HTTP"})
		case errors.Is(err, instance.ErrRequestTimeout):
			writeJSON(w, http.StatusGatewayTimeout, ErrorResponse{Error: "the machine took too long to answer"})
		default:
			writeInstanceError(w, r, err)
		}
		return
	}

	out := PortCallResponse{
		Status:     resp.Status,
		Headers:    resp.Headers,
		Truncated:  resp.Truncated,
		DurationMS: resp.Duration.Milliseconds(),
	}
	// JSON carries text. Anything else goes as base64, and says so. A body cut
	// short may end mid-character, which doesn't make it any less text.
	if utf8.Valid(resp.Body) || (resp.Truncated && utf8.Valid(trimPartialRune(resp.Body))) {
		out.Body = string(trimPartialRune(resp.Body))
	} else {
		out.Body = base64.StdEncoding.EncodeToString(resp.Body)
		out.Base64 = true
	}

	writeJSON(w, http.StatusOK, out)
}

// trimPartialRune drops a character left incomplete at the end of b, as
// happens when text is cut at a number of bytes.
func trimPartialRune(b []byte) []byte {
	for cut := 0; cut < utf8.UTFMax && cut < len(b); cut++ {
		if r, size := utf8.DecodeLastRune(b[:len(b)-cut]); r != utf8.RuneError || size != 1 {
			return b[:len(b)-cut]
		}
	}
	return b
}

func toInstanceResponse(inst *store.Instance) InstanceResponse {
	return InstanceResponse{
		ID:        inst.ID,
		Name:      inst.Name,
		Status:    inst.Status,
		CPUs:      inst.CPUs,
		MemoryMB:  inst.MemoryMB,
		DiskGB:    inst.DiskGB,
		Stack:     inst.Stack,
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
	case errors.Is(err, instance.ErrInvalidSize):
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "size must be small, medium or large, and disk_gb 10, 20 or 50"})
	case errors.Is(err, instance.ErrInvalidStack):
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "stack must be base, go, cpp, dotnet or data"})
	case errors.Is(err, instance.ErrStackUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "the image for that stack has not been built on this server; run `make images`"})
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
