package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/HasanMal1k/coder/internal/store"
)

// posts returns the chat's posts, oldest first.
func (h *harness) posts(owner, chatID string) []PostPayload {
	var posts []PostPayload
	for _, raw := range h.payloads(owner, chatID, KindPost) {
		var p PostPayload
		json.Unmarshal(raw, &p)
		posts = append(posts, p)
	}
	return posts
}

// userMessages returns what the owner wrote in the chat, oldest first.
func (h *harness) userMessages(owner, chatID string) []UserPayload {
	var messages []UserPayload
	for _, raw := range h.payloads(owner, chatID, KindUser) {
		var p UserPayload
		json.Unmarshal(raw, &p)
		messages = append(messages, p)
	}
	return messages
}

// seqOf returns the sequence number of the chat's nth event of a kind.
func (h *harness) seqOf(owner, chatID, kind string, n int) int64 {
	h.t.Helper()
	events, err := h.svc.Events(context.Background(), owner, chatID, 0)
	if err != nil {
		h.t.Fatalf("Events: %v", err)
	}
	for _, e := range events {
		if e.Kind == kind {
			if n == 0 {
				return e.Seq
			}
			n--
		}
	}
	h.t.Fatalf("the chat has no %s event number %d", kind, n)
	return 0
}

// silent is a model that must not be asked anything.
func silent(t *testing.T) func(int, []Message) (Message, error) {
	return func(int, []Message) (Message, error) {
		t.Errorf("the assistant was run, and should not have been")
		return says("?"), nil
	}
}

func TestAgentsAreGivenNamesOfTheirOwn(t *testing.T) {
	h := newHarness(t, silent(t))
	ctx := context.Background()
	first, second := h.chat("alice"), h.chat("alice")

	backend, err := h.svc.ensureAgent(ctx, "alice", first, "id-api", "backend")
	if err != nil || backend.Handle != "tom" {
		t.Fatalf("first agent = %+v, %v; want it called tom", backend, err)
	}
	again, _ := h.svc.ensureAgent(ctx, "alice", first, "id-api", "backend")
	byHandle, _ := h.svc.ensureAgent(ctx, "alice", first, "id-api", "tom")
	if again.ID != backend.ID || byHandle.ID != backend.ID {
		t.Errorf("the same job, and its handle, should be the same agent")
	}

	frontend, _ := h.svc.ensureAgent(ctx, "alice", first, "id-web", "frontend")
	elsewhere, _ := h.svc.ensureAgent(ctx, "alice", second, "id-api", "backend")
	someoneElse, _ := h.svc.ensureAgent(ctx, "bob", h.chat("bob"), "id-api", "backend")
	if frontend.Handle != "ava" || elsewhere.Handle != "max" {
		t.Errorf("handles = %q, %q; no two of one owner's agents share one, in any chat", frontend.Handle, elsewhere.Handle)
	}
	if someoneElse.Handle != "tom" {
		t.Errorf("another owner's first agent = %q, want tom", someoneElse.Handle)
	}
}

func TestNextHandleAndMentions(t *testing.T) {
	if got := nextHandle(nil); got != "tom" {
		t.Errorf("nextHandle(nil) = %q", got)
	}
	if got := nextHandle([]string{"tom", "max"}); got != "ava" {
		t.Errorf("nextHandle(tom, max) = %q", got)
	}
	if got := nextHandle(handles); got != "tom2" {
		t.Errorf("with every name taken, nextHandle = %q, want tom2", got)
	}

	got := mentions("@Tom, can you and @ava- look? Mail me@tom.com, and @tom again.\n@kai")
	if !equal(got, []string{"tom", "ava", "kai"}) {
		t.Errorf("mentions = %q", got)
	}
	if got := mentions("no one here, not even a.b@c"); len(got) != 0 {
		t.Errorf("mentions = %q, want none", got)
	}
}

func TestWritingToAnAgentGoesStraightToIt(t *testing.T) {
	h := newHarness(t, silent(t))
	ctx := context.Background()
	h.machines.Create(ctx, "alice", MachineSpec{Name: "api"}, func(string) {})
	id := h.chat("alice")
	h.svc.ensureAgent(ctx, "alice", id, "id-api", "backend")

	var task string
	h.machines.runTask = func(_ context.Context, _, t string, progress func(string)) (string, error) {
		task = t
		progress("Reading server.py")
		return "It listens on port 8000.", nil
	}

	if err := h.svc.SendMessage(ctx, "alice", id, UserMessage{Text: "@tom which port?"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	eventually(t, "tom's answer", func() bool { return len(h.posts("alice", id)) == 1 })

	seq := h.seqOf("alice", id, KindUser, 0)
	post := h.posts("alice", id)[0]
	if post.Agent != "tom" || post.Role != "backend" || post.Machine.Name != "api" || post.ReplyTo != seq || post.Text != "It listens on port 8000." {
		t.Errorf("post = %+v; want tom's report, as the reply to message %d", post, seq)
	}
	if msg := h.userMessages("alice", id)[0]; !equal(msg.To, []string{"tom"}) {
		t.Errorf("the message was recorded as for %q, want tom", msg.To)
	}
	if !strings.Contains(task, "The owner wrote to you directly") || !strings.Contains(task, "@tom which port?") {
		t.Errorf("tom was given %q", task)
	}

	var activity ActivityPayload
	json.Unmarshal(h.payloads("alice", id, KindActivity)[0], &activity)
	if activity.ToolCallID != directCallID(seq, "tom") || activity.Text != "Reading server.py" {
		t.Errorf("activity = %+v; tom's work is recorded under its answer", activity)
	}

	chat, _ := h.svc.GetChat(ctx, "alice", id)
	if chat.Status != StatusIdle || len(h.llm.calls) != 0 {
		t.Errorf("status %q after %d model calls; the assistant only reads such messages", chat.Status, len(h.llm.calls))
	}
	if agent := h.agents.byName(id, "backend"); agent.Status != AgentIdle {
		t.Errorf("tom is %s after answering", agent.Status)
	}

	// The next time the assistant runs, it reads both.
	h.llm.reply = func(int, []Message) (Message, error) { return says("Noted."), nil }
	h.send("alice", id, "where are we?")
	h.settle("alice", id)

	var told []string
	for _, m := range h.llm.sent(0) {
		told = append(told, m.Content)
	}
	all := strings.Join(told, "\n")
	for _, want := range []string{
		"The user wrote this in the chat to @tom directly",
		"@tom which port?",
		`tom (the "backend" agent on "api") answered the user in the chat:`,
		"It listens on port 8000.",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("the assistant was not told %q:\n%s", want, all)
		}
	}
}

func TestAMessageToABusyAgentWaitsForIt(t *testing.T) {
	h := newHarness(t, silent(t))
	ctx := context.Background()
	h.machines.Create(ctx, "alice", MachineSpec{Name: "api"}, func(string) {})
	id := h.chat("alice")
	h.svc.ensureAgent(ctx, "alice", id, "id-api", "backend")

	release := make(chan struct{})
	tasks := make(chan string, 2)
	h.machines.runTask = func(_ context.Context, _, task string, _ func(string)) (string, error) {
		tasks <- task
		if strings.Contains(task, "first") {
			<-release
			return "Did the first.", nil
		}
		return "Did the second.", nil
	}

	h.svc.SendMessage(ctx, "alice", id, UserMessage{Text: "@tom first"})
	<-tasks
	h.svc.SendMessage(ctx, "alice", id, UserMessage{Text: "@tom second"})
	eventually(t, "word that tom is busy", func() bool { return len(h.posts("alice", id)) == 1 })
	if post := h.posts("alice", id)[0]; !post.Queued || post.ReplyTo != h.seqOf("alice", id, KindUser, 1) {
		t.Errorf("post = %+v; want it to say the second message waits", post)
	}

	close(release)
	if second := <-tasks; !strings.Contains(second, "second") {
		t.Errorf("after the first, tom was given %q", second)
	}
	eventually(t, "both answers", func() bool { return len(h.posts("alice", id)) == 3 })

	posts := h.posts("alice", id)
	if posts[1].Text != "Did the first." || posts[1].ReplyTo != h.seqOf("alice", id, KindUser, 0) {
		t.Errorf("first answer = %+v", posts[1])
	}
	if posts[2].Text != "Did the second." || posts[2].ReplyTo != h.seqOf("alice", id, KindUser, 1) {
		t.Errorf("second answer = %+v", posts[2])
	}
}

func TestWhoAMessageIsFor(t *testing.T) {
	h := newHarness(t, func(int, []Message) (Message, error) { return says("Hello."), nil })
	ctx := context.Background()
	h.machines.Create(ctx, "alice", MachineSpec{Name: "api"}, func(string) {})
	id := h.chat("alice")
	h.svc.ensureAgent(ctx, "alice", id, "id-api", "backend")

	err := h.svc.SendMessage(ctx, "alice", id, UserMessage{Text: "hi", To: []string{"zed"}})
	if !errors.Is(err, ErrUnknownRecipient) {
		t.Errorf("a message for nobody in the chat: %v, want ErrUnknownRecipient", err)
	}

	// A mention of a name nobody has is just a word: the assistant gets it.
	h.send("alice", id, "ask @zed about it")
	h.settle("alice", id)
	if len(h.llm.calls) != 1 || len(h.posts("alice", id)) != 0 {
		t.Errorf("%d model calls and %d posts; it was for the assistant", len(h.llm.calls), len(h.posts("alice", id)))
	}

	// Both at once: the assistant runs, and tom answers too.
	h.machines.runTask = func(context.Context, string, string, func(string)) (string, error) { return "On it.", nil }
	if err := h.svc.SendMessage(ctx, "alice", id, UserMessage{Text: "@assistant @tom ship it"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	h.settle("alice", id)
	eventually(t, "tom's answer", func() bool { return len(h.posts("alice", id)) == 1 })
	if len(h.llm.calls) != 2 {
		t.Errorf("%d model calls, want 2", len(h.llm.calls))
	}
	last := h.llm.sent(1)
	if got := last[len(last)-1].Content; !strings.Contains(got, "to you and to @tom") {
		t.Errorf("the assistant was told %q", got)
	}
}

func TestFilesGoToTheAssistantAndOnToAgents(t *testing.T) {
	ctx := context.Background()
	var (
		h      *harness
		logo   string
		toldIt string
	)
	h = newHarness(t, func(call int, messages []Message) (Message, error) {
		if call == 0 {
			toldIt = messages[len(messages)-1].Content
			return wants(callTool("c1", toolCodeOnMachine, map[string]any{
				"machine": "api", "agent": "frontend", "task": "use the logo", "files": []string{logo},
			})), nil
		}
		return says("Done."), nil
	})
	h.machines.Create(ctx, "alice", MachineSpec{Name: "api"}, func(string) {})
	id := h.chat("alice")

	spec, err := h.svc.UploadAttachment(ctx, "alice", id, "../specs/spec.md", []byte("# Shop\n\nSell socks."))
	if err != nil {
		t.Fatalf("UploadAttachment: %v", err)
	}
	png, _ := h.svc.UploadAttachment(ctx, "alice", id, "logo.png", []byte("\x89PNG\r\n\x1a\n rest of it"))
	logo = png.ID
	if spec.Name != "spec.md" || spec.ContentType != "text/markdown; charset=utf-8" || png.ContentType != "image/png" {
		t.Errorf("stored as %q (%s) and %q (%s)", spec.Name, spec.ContentType, png.Name, png.ContentType)
	}

	err = h.svc.SendMessage(ctx, "alice", id, UserMessage{Text: "build this", Attachments: []string{spec.ID, png.ID}})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	h.settle("alice", id)

	for _, want := range []string{"build this", "spec.md (text/markdown", "Sell socks.", "logo.png (image/png", "id " + png.ID, "You cannot see images"} {
		if !strings.Contains(toldIt, want) {
			t.Errorf("the assistant was not told %q:\n%s", want, toldIt)
		}
	}

	where := fmt.Sprintf("/home/coder/.coder/attachments/%s/logo.png", png.ID[:8])
	if data := h.machines.files["id-api:"+where]; string(data) != "\x89PNG\r\n\x1a\n rest of it" {
		t.Errorf("on the machine at %s: %q", where, data)
	}
	if len(h.machines.tasks) != 1 || !strings.Contains(h.machines.tasks[0], where) {
		t.Errorf("the task does not say where the file is: %q", h.machines.tasks)
	}

	if refs := h.userMessages("alice", id)[0].Attachments; len(refs) != 2 || refs[1].Text != "" || refs[0].Text == "" {
		t.Errorf("attachments in the message = %+v; only the text file's contents go with it", refs)
	}

	// What can't be sent.
	if _, err := h.svc.UploadAttachment(ctx, "alice", id, "empty.txt", nil); !errors.Is(err, ErrInvalidAttachment) {
		t.Errorf("an empty file: %v", err)
	}
	other := h.chat("alice")
	if err := h.svc.SendMessage(ctx, "alice", other, UserMessage{Attachments: []string{png.ID}}); !errors.Is(err, ErrAttachmentNotFound) {
		t.Errorf("another chat's file: %v", err)
	}
	if _, err := h.svc.Attachment(ctx, "bob", id, png.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("someone else reading it: %v", err)
	}
}

func TestAgentsWriteInTheChat(t *testing.T) {
	h := newHarness(t, silent(t))
	ctx := context.Background()
	h.machines.Create(ctx, "alice", MachineSpec{Name: "web"}, func(string) {})
	id := h.chat("alice")
	h.svc.ensureAgent(ctx, "alice", id, "id-web", "frontend")
	h.machines.files = map[string][]byte{"id-web:/home/coder/mock-a.png": []byte("\x89PNG\r\n\x1a\nA")}
	machine := &store.Instance{ID: "id-web", OwnerID: "alice", Name: "web"}

	out, err := h.svc.PostToChat(ctx, machine, AgentPost{
		Agent: "tom", Text: "Which of these should we follow?", Question: true, Options: []string{"Minimal", "Playful"},
		Files: []string{"/home/coder/mock-a.png"}, Port: 3000, Path: "mockups/b.html", Label: "Mockup B",
	})
	if err != nil || !strings.Contains(out, "Your question is in the chat") {
		t.Fatalf("PostToChat = %q, %v", out, err)
	}

	posts := h.posts("alice", id)
	if len(posts) != 1 {
		t.Fatalf("%d posts", len(posts))
	}
	post := posts[0]
	if post.Agent != "tom" || !post.Question || !equal(post.Options, []string{"Minimal", "Playful"}) || post.ReplyTo != 0 {
		t.Errorf("post = %+v", post)
	}
	if post.Preview == nil || *post.Preview != (PostPreview{Port: 3000, Path: "/mockups/b.html", Label: "Mockup B"}) {
		t.Errorf("preview = %+v", post.Preview)
	}
	if len(post.Attachments) != 1 || post.Attachments[0].Name != "mock-a.png" || post.Attachments[0].ContentType != "image/png" {
		t.Fatalf("attachments = %+v", post.Attachments)
	}
	if a, err := h.svc.Attachment(ctx, "alice", id, post.Attachments[0].ID); err != nil || string(a.Data) != "\x89PNG\r\n\x1a\nA" {
		t.Errorf("the mockup in the chat: %v", err)
	}

	// The answer goes to tom.
	answered := make(chan string, 1)
	h.machines.runTask = func(_ context.Context, _, task string, _ func(string)) (string, error) {
		answered <- task
		return "Going with minimal.", nil
	}
	h.svc.SendMessage(ctx, "alice", id, UserMessage{Text: "Minimal", To: []string{"tom"}})
	if task := <-answered; !strings.Contains(task, "Minimal") {
		t.Errorf("tom was given %q", task)
	}

	// Mistakes are the agent's to put right.
	var mistake Mistake
	if _, err := h.svc.PostToChat(ctx, machine, AgentPost{Agent: "zed", Text: "hi"}); !errors.As(err, &mistake) || !strings.Contains(err.Error(), "tom (frontend)") {
		t.Errorf("an unknown name: %v", err)
	}
	if _, err := h.svc.PostToChat(ctx, machine, AgentPost{Agent: "frontend", Text: "see", Files: []string{"/nope.png"}}); !errors.As(err, &mistake) || !strings.Contains(err.Error(), "Could not attach /nope.png") {
		t.Errorf("a missing file: %v", err)
	}
	if _, err := h.svc.PostToChat(ctx, machine, AgentPost{Agent: "tom", Text: " "}); !errors.As(err, &mistake) {
		t.Errorf("nothing to say: %v", err)
	}
}

func TestFileNames(t *testing.T) {
	for in, want := range map[string]string{
		"logo.png":                        "logo.png",
		"../../etc/passwd":                "passwd",
		`C:\Users\me\cv.pdf`:              "cv.pdf",
		".env":                            "env",
		"":                                "file",
		"a\x00b\nc.txt":                   "a_b_c.txt",
		strings.Repeat("x", 300) + ".png": strings.Repeat("x", 196) + ".png",
	} {
		if got := fileName(in); got != want {
			t.Errorf("fileName(%q) = %q, want %q", in, got, want)
		}
	}
}
