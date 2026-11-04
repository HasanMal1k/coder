package httpapi

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/HasanMal1k/coder/internal/assistant"
	"github.com/HasanMal1k/coder/internal/middleware"
	"github.com/HasanMal1k/coder/internal/store"
)

type AgentHandler struct {
	svc *assistant.Service
}

type AgentMachineResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AgentResponse struct {
	ID string `json:"id"`
	// Name is the agent's job ("frontend"); Handle is its own name ("tom"),
	// by which the owner addresses it in its chat.
	Name    string               `json:"name"`
	Handle  string               `json:"handle"`
	ChatID  string               `json:"chat_id"`
	Machine AgentMachineResponse `json:"machine"`
	// Status is "idle", "working", "failed" or "stopped".
	Status string `json:"status"`
	// Task is what it is working on, or last worked on.
	Task       string `json:"task"`
	Background bool   `json:"background"`
	// Activity is what it is doing right now, while working.
	Activity string `json:"activity"`
	// Report is what came of its last task: its report, or why it failed.
	Report     string     `json:"report"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type AgentListResponse struct {
	Agents []AgentResponse `json:"agents"`
}

func NewAgentHandler(svc *assistant.Service) *AgentHandler {
	return &AgentHandler{
		svc: svc,
	}
}

// List returns the user's agents, from every chat: those working first.
func (h *AgentHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	agents, err := h.svc.ListAgents(r.Context(), user.ID)
	if err != nil {
		writeAgentError(w, r, err)
		return
	}

	resp := AgentListResponse{Agents: []AgentResponse{}}
	for _, agent := range agents {
		resp.Agents = append(resp.Agents, toAgentResponse(agent))
	}

	writeJSON(w, http.StatusOK, resp)
}

// Stop stops what the agent is doing. Stopping an agent that isn't working
// succeeds and changes nothing.
func (h *AgentHandler) Stop(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	if err := h.svc.StopAgent(r.Context(), user.ID, chi.URLParam(r, "id")); err != nil {
		writeAgentError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func toAgentResponse(a *store.Agent) AgentResponse {
	return AgentResponse{
		ID:         a.ID,
		Name:       a.Name,
		Handle:     a.Handle,
		ChatID:     a.ChatID,
		Machine:    AgentMachineResponse{ID: a.InstanceID, Name: a.MachineName},
		Status:     a.Status,
		Task:       a.Task,
		Background: a.Background,
		Activity:   a.Activity,
		Report:     a.Report,
		StartedAt:  a.StartedAt,
		FinishedAt: a.FinishedAt,
		UpdatedAt:  a.UpdatedAt,
	}
}

func writeAgentError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, assistant.ErrAgentNotFound):
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "agent not found"})
	default:
		log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
	}
}
