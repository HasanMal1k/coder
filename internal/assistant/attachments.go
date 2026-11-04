package assistant

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/HasanMal1k/coder/internal/guestapi"
	"github.com/HasanMal1k/coder/internal/store"
)

const (
	// MaxAttachmentSize is the largest file that can be put in a chat. It is
	// what a machine can be given or asked for in one go.
	MaxAttachmentSize = guestapi.MaxFileSize
	// maxAttachments is how many files one message can carry.
	maxAttachments = 10
	// maxPostFiles is how many files an agent can put in one post.
	maxPostFiles = 5
	// maxInlineText is how much of a text file the assistant is shown,
	// and maxInlineTotal how much of all of one message's.
	maxInlineText  = 16 << 10
	maxInlineTotal = 48 << 10
	maxFileName    = 200

	// attachmentsFolder is where on a machine files given to its agents go,
	// each in a folder of its own.
	attachmentsFolder = "/home/coder/.coder/attachments"
)

var (
	// ErrAttachmentNotFound is returned for a file that is not in the chat.
	ErrAttachmentNotFound = errors.New("attachment not found")
	// ErrInvalidAttachment is returned for a file that is empty or too
	// large.
	ErrInvalidAttachment = errors.New("invalid attachment")
)

// Attachments is where the files put in chats are kept.
// *store.AttachmentRepository implements it.
type Attachments interface {
	Create(ctx context.Context, a *store.Attachment) error
	Get(ctx context.Context, id, chatID, ownerID string) (*store.Attachment, error)
}

// AttachmentRef is how an event refers to a file in the chat.
type AttachmentRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	// Text is the start of a text file's contents, for the assistant to
	// read. Empty for anything else.
	Text string `json:"text,omitempty"`
}

// TaskFile is a file to put on a machine before its agent is given a task.
type TaskFile struct {
	// Path is absolute.
	Path string
	Data []byte
}

// UploadAttachment stores a file the owner is about to send in a chat.
func (s *Service) UploadAttachment(ctx context.Context, ownerID, chatID, name string, data []byte) (*store.Attachment, error) {
	chat, err := s.GetChat(ctx, ownerID, chatID)
	if err != nil {
		return nil, err
	}

	return s.storeAttachment(ctx, ownerID, chat.ID, name, data)
}

func (s *Service) storeAttachment(ctx context.Context, ownerID, chatID, name string, data []byte) (*store.Attachment, error) {
	if len(data) == 0 || len(data) > MaxAttachmentSize {
		return nil, ErrInvalidAttachment
	}

	name = fileName(name)
	a := &store.Attachment{
		OwnerID:     ownerID,
		ChatID:      chatID,
		Name:        name,
		ContentType: contentType(name, data),
		Data:        data,
	}
	if err := s.attachments.Create(ctx, a); err != nil {
		return nil, err
	}

	return a, nil
}

// Attachment returns one of a chat's files, with its contents.
func (s *Service) Attachment(ctx context.Context, ownerID, chatID, id string) (*store.Attachment, error) {
	if uuid.Validate(id) != nil {
		return nil, ErrAttachmentNotFound
	}
	chat, err := s.GetChat(ctx, ownerID, chatID)
	if err != nil {
		return nil, err
	}

	a, err := s.attachments.Get(ctx, id, chat.ID, ownerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrAttachmentNotFound
		}
		return nil, err
	}

	return a, nil
}

// attachmentRefs looks up the chat's files by ID, for an event to refer to.
func (s *Service) attachmentRefs(ctx context.Context, ownerID, chatID string, ids []string) ([]AttachmentRef, error) {
	if len(ids) > maxAttachments {
		return nil, ErrInvalidMessage
	}

	refs := make([]AttachmentRef, 0, len(ids))
	inlined := 0
	for _, id := range ids {
		a, err := s.Attachment(ctx, ownerID, chatID, id)
		if err != nil {
			return nil, err
		}

		ref := refOf(a)
		if readable(a.ContentType, a.Data) && inlined < maxInlineTotal {
			text := head(string(a.Data), min(maxInlineText, maxInlineTotal-inlined))
			ref.Text = text
			inlined += len(text)
		}
		refs = append(refs, ref)
	}

	return refs, nil
}

func refOf(a *store.Attachment) AttachmentRef {
	return AttachmentRef{ID: a.ID, Name: a.Name, ContentType: a.ContentType, Size: a.Size}
}

// taskFiles loads the chat's files by ID to put on a machine, and says in a
// few lines, for the task, where they will be.
func (s *Service) taskFiles(ctx context.Context, ownerID, chatID string, ids []string) ([]TaskFile, string, error) {
	if len(ids) == 0 {
		return nil, "", nil
	}
	if len(ids) > maxAttachments {
		return nil, "", fmt.Errorf("at most %d files can go with one task", maxAttachments)
	}

	var (
		files []TaskFile
		b     strings.Builder
	)
	b.WriteString("Files the owner gave for this, now on this machine:\n")
	for _, id := range ids {
		a, err := s.Attachment(ctx, ownerID, chatID, strings.TrimSpace(id))
		if err != nil {
			if errors.Is(err, ErrAttachmentNotFound) {
				return nil, "", Mistake(fmt.Sprintf("There is no file %q in this conversation. Use the ids given beside the files the user attached.", id))
			}
			return nil, "", err
		}

		// A folder of its own for each, so two files of one name don't
		// meet.
		where := path.Join(attachmentsFolder, a.ID[:8], a.Name)
		files = append(files, TaskFile{Path: where, Data: a.Data})
		fmt.Fprintf(&b, "- %s (%s, %s)\n", where, a.ContentType, sizeText(a.Size))
	}

	return files, b.String(), nil
}

// describeAttachments says, for the assistant, what files came with a message,
// with the contents of those it can read.
func describeAttachments(refs []AttachmentRef) string {
	if len(refs) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n[Files attached to this message. You cannot see images; to give any of these files to a coding agent, list their ids in code_on_machine's files.]\n")
	for _, ref := range refs {
		fmt.Fprintf(&b, "- %s (%s, %s), id %s\n", ref.Name, ref.ContentType, sizeText(ref.Size), ref.ID)
	}
	for _, ref := range refs {
		if ref.Text == "" {
			continue
		}
		fmt.Fprintf(&b, "\n--- %s ---\n%s\n", ref.Name, ref.Text)
		if len(ref.Text) < ref.Size {
			b.WriteString("[The rest of the file is not shown.]\n")
		}
		fmt.Fprintf(&b, "--- end of %s ---\n", ref.Name)
	}

	return strings.TrimRight(b.String(), "\n")
}

// fileName makes a name safe to keep and to write on a machine: no folders,
// nothing hidden, and not too long.
func fileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimLeft(name, ".")

	if name == "" || name == "/" {
		name = "file"
	}
	if len(name) > maxFileName {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = strings.ToValidUTF8(name[:maxFileName-len(ext)], "") + ext
	}

	return name
}

// textTypes are the extensions of text files the sniffing below would
// mistake for something else, or call plain.
var textTypes = map[string]string{
	".md":       "text/markdown; charset=utf-8",
	".markdown": "text/markdown; charset=utf-8",
	".csv":      "text/csv; charset=utf-8",
	".json":     "application/json",
	".yaml":     "text/yaml; charset=utf-8",
	".yml":      "text/yaml; charset=utf-8",
	".svg":      "image/svg+xml",
	".ts":       "text/plain; charset=utf-8",
	".tsx":      "text/plain; charset=utf-8",
}

// contentType says what a file is, from its name and its first bytes.
func contentType(name string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(name))
	if t, ok := textTypes[ext]; ok {
		return t
	}

	sniffed := http.DetectContentType(data)
	// Sniffing only tells text from binary for most formats; the name says
	// more, but only for text, never to make a binary file into something a
	// browser would run.
	if strings.HasPrefix(sniffed, "text/plain") {
		if byName := mime.TypeByExtension(ext); strings.HasPrefix(byName, "text/") && !strings.HasPrefix(byName, "text/html") {
			return byName
		}
	}

	return sniffed
}

// readable reports whether a file is text the assistant can be shown.
func readable(contentType string, data []byte) bool {
	text := strings.HasPrefix(contentType, "text/") ||
		strings.HasPrefix(contentType, "application/json") ||
		strings.HasPrefix(contentType, "image/svg+xml") ||
		strings.HasPrefix(contentType, "application/xml")

	return text && utf8.Valid(data[:min(len(data), maxInlineText)])
}

// head is the start of s, at most max bytes of it, cut between characters.
func head(s string, max int) string {
	if len(s) <= max {
		return s
	}

	return strings.ToValidUTF8(s[:max], "")
}

// sizeText says how big a file is, roughly.
func sizeText(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d bytes", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%d KB", (bytes+512)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
}
