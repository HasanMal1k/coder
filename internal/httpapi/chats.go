package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/HasanMal1k/coder/internal/assistant"
	"github.com/HasanMal1k/coder/internal/middleware"
	"github.com/HasanMal1k/coder/internal/store"
)

// streamHeartbeat is how often an idle event stream sends a comment, so that
// anything between the server and the browser keeps the connection open.
const streamHeartbeat = 20 * time.Second

// maxMessageBody caps the body of a request that sends a message.
const maxMessageBody = 256 * 1024

type ChatHandler struct {
	svc *assistant.Service
	// model is the name of the model in use, shown in the interface.
	model string
}

type AssistantResponse struct {
	// Enabled is false when the server has no model configured: chats can
	// be read but not continued.
	Enabled bool   `json:"enabled"`
	Model   string `json:"model,omitempty"`
}

type ChatResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ChatListResponse struct {
	Chats []ChatResponse `json:"chats"`
}

type ChatEventResponse struct {
	Seq  int64  `json:"seq"`
	Kind string `json:"kind"`
	// Payload's shape depends on Kind; see docs/API.md.
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type ChatDetailResponse struct {
	Chat   ChatResponse        `json:"chat"`
	Events []ChatEventResponse `json:"events"`
}

// DraftResponse is the message the assistant is writing, so far.
type DraftResponse struct {
	Text string `json:"text"`
}

type SendMessageRequest struct {
	Text string `json:"text"`
}

func NewChatHandler(svc *assistant.Service, model string) *ChatHandler {
	return &ChatHandler{
		svc:   svc,
		model: model,
	}
}

// Info says whether the assistant can be used.
func (h *ChatHandler) Info(w http.ResponseWriter, r *http.Request) {
	resp := AssistantResponse{Enabled: h.svc.Enabled()}
	if resp.Enabled {
		resp.Model = h.model
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *ChatHandler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	chats, err := h.svc.ListChats(r.Context(), user.ID)
	if err != nil {
		writeChatError(w, r, err)
		return
	}

	resp := ChatListResponse{Chats: []ChatResponse{}}
	for _, chat := range chats {
		resp.Chats = append(resp.Chats, toChatResponse(chat))
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *ChatHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	chat, err := h.svc.CreateChat(r.Context(), user.ID)
	if err != nil {
		writeChatError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, toChatResponse(chat))
}

// Get returns a chat with its whole history.
func (h *ChatHandler) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")

	chat, err := h.svc.GetChat(r.Context(), user.ID, id)
	if err != nil {
		writeChatError(w, r, err)
		return
	}

	events, err := h.svc.Events(r.Context(), user.ID, id, 0)
	if err != nil {
		writeChatError(w, r, err)
		return
	}

	resp := ChatDetailResponse{
		Chat:   toChatResponse(chat),
		Events: []ChatEventResponse{},
	}
	for _, event := range events {
		resp.Events = append(resp.Events, toChatEventResponse(event))
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *ChatHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	if err := h.svc.DeleteChat(r.Context(), user.ID, chi.URLParam(r, "id")); err != nil {
		writeChatError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Send adds the user's message to a chat. The assistant's work on it happens
// afterwards, in the background: this returns as soon as the message is
// accepted, and Stream is how the reply is followed.
func (h *ChatHandler) Send(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	var req SendMessageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMessageBody)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if err := h.svc.Send(r.Context(), user.ID, chi.URLParam(r, "id"), req.Text); err != nil {
		writeChatError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// Cancel stops the assistant, or withdraws the question it was waiting on.
func (h *ChatHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	if err := h.svc.Cancel(r.Context(), user.ID, chi.URLParam(r, "id")); err != nil {
		writeChatError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Stream follows a chat as server-sent events: an "event" for each entry in
// its history after ?after=<seq>, a "chat" whenever its status or title
// changes, and a "draft" as the assistant writes its next message. It stays open until the client goes away or the chat is deleted.
//
// A client that loses the connection reconnects with the last seq it saw and
// misses nothing: events are read from the database, never from memory.
func (h *ChatHandler) Stream(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	var after int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "after must be a sequence number"})
			return
		}
		after = parsed
	}

	ctx := r.Context()

	// Checked before the stream starts, while a plain error can still be
	// sent.
	found, err := h.svc.GetChat(ctx, user.ID, chi.URLParam(r, "id"))
	if err != nil {
		writeChatError(w, r, err)
		return
	}
	id := found.ID

	// Watch first, then read: a change between the two wakes the loop for
	// another look instead of being missed.
	changed, stop := h.svc.Watch(id)
	defer stop()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// Tells nginx-style proxies not to hold the stream back.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher := http.NewResponseController(w)

	heartbeat := time.NewTicker(streamHeartbeat)
	defer heartbeat.Stop()

	var (
		sent      ChatResponse
		sentDraft string
	)

	for {
		chat, err := h.svc.GetChat(ctx, user.ID, id)
		if err != nil {
			if errors.Is(err, assistant.ErrNotFound) {
				writeEvent(w, "deleted", struct{}{})
				flusher.Flush()
			} else if ctx.Err() == nil {
				log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
			}
			return
		}

		events, err := h.svc.Events(ctx, user.ID, id, after)
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, assistant.ErrNotFound) {
				log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
			}
			return
		}

		for _, event := range events {
			if writeEvent(w, "event", toChatEventResponse(event)) != nil {
				return
			}
			after = event.Seq
		}

		// The status goes after the events that led to it, so a client never
		// sees "idle" before the assistant's last message. UpdatedAt changes
		// with every event and isn't worth announcing by itself.
		current := toChatResponse(chat)
		if current.Status != sent.Status || current.Title != sent.Title {
			if writeEvent(w, "chat", current) != nil {
				return
			}
			sent = current
		}

		// What the assistant is in the middle of writing. Sent whole each
		// time: it is short, and a client that joins part-way needs all of it.
		// An empty one says the message is finished (it is among the events
		// above) or was abandoned.
		if draft := h.svc.Draft(id); draft != sentDraft {
			if writeEvent(w, "draft", DraftResponse{Text: draft}) != nil {
				return
			}
			sentDraft = draft
		}

		if flusher.Flush() != nil {
			return
		}

	wait:
		for {
			select {
			case <-changed:
				break wait
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
					return
				}
				if flusher.Flush() != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
}

// Architecture returns the chat's design and how what it describes stands:
// which machines exist, and whether each component is answering.
func (h *ChatHandler) Architecture(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	arch, err := h.svc.Architecture(r.Context(), user.ID, chi.URLParam(r, "id"))
	if err != nil {
		writeChatError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, arch)
}

// ApproveArchitectureRequest carries the design as the user changed it. An
// empty body approves the design as proposed.
type ApproveArchitectureRequest struct {
	Design *assistant.Design `json:"design"`
}

// ApproveArchitecture records that the user agrees to the design the
// assistant proposed, and sets the assistant to building it.
func (h *ChatHandler) ApproveArchitecture(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	var req ApproveArchitectureRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMessageBody)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if err := h.svc.ApproveArchitecture(r.Context(), user.ID, chi.URLParam(r, "id"), req.Design); err != nil {
		writeChatError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// writeEvent writes one server-sent event with a JSON body.
func writeEvent(w http.ResponseWriter, name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	// JSON never contains a raw newline, so the data is always one line.
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)

	return err
}

func toChatResponse(chat *store.Chat) ChatResponse {
	return ChatResponse{
		ID:        chat.ID,
		Title:     chat.Title,
		Status:    chat.Status,
		CreatedAt: chat.CreatedAt,
		UpdatedAt: chat.UpdatedAt,
	}
}

func toChatEventResponse(event *store.ChatEvent) ChatEventResponse {
	return ChatEventResponse{
		Seq:       event.Seq,
		Kind:      event.Kind,
		Payload:   event.Payload,
		CreatedAt: event.CreatedAt,
	}
}

func writeChatError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, assistant.ErrNotFound):
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "chat not found"})
	case errors.Is(err, assistant.ErrNoDesign):
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "this chat has no design waiting"})
	case errors.Is(err, assistant.ErrInvalidDesign):
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	case errors.Is(err, assistant.ErrBusy):
		writeJSON(w, http.StatusConflict, ErrorResponse{Error: "the assistant is still working on your last message"})
	case errors.Is(err, assistant.ErrInvalidMessage):
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "message must be between 1 and 20000 characters"})
	case errors.Is(err, assistant.ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "the assistant is not set up on this server"})
	default:
		log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
	}
}
