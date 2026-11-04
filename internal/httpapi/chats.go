package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
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
	// To names who the message is for, by handle ("tom", "assistant").
	// Left out, it is whoever the text mentions with an @, or the assistant.
	To []string `json:"to,omitempty"`
	// Attachments are the IDs of files uploaded to the chat for it.
	Attachments []string `json:"attachments,omitempty"`
}

// AttachmentResponse describes a file uploaded to a chat.
type AttachmentResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int       `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
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

	msg := assistant.UserMessage{Text: req.Text, To: req.To, Attachments: req.Attachments}
	if err := h.svc.SendMessage(r.Context(), user.ID, chi.URLParam(r, "id"), msg); err != nil {
		writeChatError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// Upload stores a file to send in a chat: the request is a multipart form
// with the file in the field "file". The file is sent by giving its ID with
// a message.
func (h *ChatHandler) Upload(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	// The form's own fields and boundaries take a little room besides the
	// file.
	r.Body = http.MaxBytesReader(w, r.Body, assistant.MaxAttachmentSize+64<<10)
	file, header, err := r.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, ErrorResponse{Error: fmt.Sprintf("a file can be at most %d MB", assistant.MaxAttachmentSize>>20)})
			return
		}
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: `the request must be a multipart form with the file in "file"`})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, assistant.MaxAttachmentSize+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "could not read the file"})
		return
	}

	a, err := h.svc.UploadAttachment(r.Context(), user.ID, chi.URLParam(r, "id"), header.Filename, data)
	if err != nil {
		writeChatError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, AttachmentResponse{
		ID: a.ID, Name: a.Name, ContentType: a.ContentType, Size: a.Size, CreatedAt: a.CreatedAt,
	})
}

// shownInline are the types a browser is let show as they are. Anything
// else is sent as a download, so that nothing in a chat can run as a page of
// this site.
var shownInline = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// Attachment sends one of a chat's files.
func (h *ChatHandler) Attachment(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "not authenticated"})
		return
	}

	a, err := h.svc.Attachment(r.Context(), user.ID, chi.URLParam(r, "id"), chi.URLParam(r, "attachment"))
	if err != nil {
		writeChatError(w, r, err)
		return
	}

	// An SVG among the rest: the app shows it as a picture, which runs
	// nothing in it, but opened by itself it is a document that can.
	disposition := "attachment"
	if shownInline[a.ContentType] {
		disposition = "inline"
	}

	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.Name}))
	w.Header().Set("Content-Length", strconv.Itoa(len(a.Data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// A file never changes once uploaded.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	w.Write(a.Data)
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
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "a message needs words or a file, at most 20000 characters and 10 files"})
	case errors.Is(err, assistant.ErrAttachmentNotFound):
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "file not found"})
	case errors.Is(err, assistant.ErrInvalidAttachment):
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("a file must not be empty, and can be at most %d MB", assistant.MaxAttachmentSize>>20)})
	case errors.Is(err, assistant.ErrUnknownRecipient):
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	case errors.Is(err, assistant.ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "the assistant is not set up on this server"})
	default:
		log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
	}
}
