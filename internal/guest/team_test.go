package guest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HasanMal1k/coder/internal/guestapi"
)

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func TestTeamSyncKeepsACopyOnDisk(t *testing.T) {
	files := map[string]string{
		"TEAM.md":                 "# The team\n",
		"agents/api--backend.md":  "built the API\n",
		"agents/web--frontend.md": "built the page\n",
	}
	etag := `"v1"`
	var requests, sent int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer instance-id.guest-token" {
			t.Errorf("Authorization = %q, want the machine's identity", r.Header.Get("Authorization"))
		}

		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		sent++
		json.NewEncoder(w).Encode(map[string]any{"files": files})
	}))
	defer server.Close()

	dir := filepath.Join(t.TempDir(), "team")
	sync := &teamSync{client: server.Client(), url: server.URL, key: "instance-id.guest-token", dir: dir}
	ctx := context.Background()

	if _, err := sync.once(ctx); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if got := read(t, filepath.Join(dir, "agents", "api--backend.md")); got != "built the API\n" {
		t.Errorf("agent file = %q", got)
	}
	if got := read(t, filepath.Join(dir, "TEAM.md")); got != "# The team\n" {
		t.Errorf("TEAM.md = %q", got)
	}

	// Nothing changed: the record is not sent again.
	if _, err := sync.once(ctx); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if requests != 2 || sent != 1 {
		t.Errorf("%d requests, %d of them answered in full; an unchanged record must not be sent twice", requests, sent)
	}

	// An agent is gone and another has more to say.
	delete(files, "agents/web--frontend.md")
	files["agents/api--backend.md"] = "built the API, then fixed it\n"
	etag = `"v2"`

	if _, err := sync.once(ctx); err != nil {
		t.Fatalf("third sync: %v", err)
	}
	if got := read(t, filepath.Join(dir, "agents", "api--backend.md")); got != "built the API, then fixed it\n" {
		t.Errorf("agent file after a change = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "agents", "web--frontend.md")); !os.IsNotExist(err) {
		t.Errorf("the file of an agent that is gone is still there (%v)", err)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

func TestTeamSyncRefusesFilesOutsideItsFolder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "team")

	for _, path := range []string{"../escaped.md", "/etc/passwd", "agents/../../escaped.md"} {
		if err := writeFiles(dir, map[string]string{"TEAM.md": "ok", path: "bad"}); err == nil {
			t.Errorf("writeFiles accepted %q", path)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "escaped.md")); !os.IsNotExist(err) {
		t.Error("a file was written outside the folder")
	}
	if _, err := os.Stat(filepath.Join(dir, "TEAM.md")); !os.IsNotExist(err) {
		t.Error("part of a refused record was written")
	}
}

func TestTeamSyncReportsAnOlderControlPlane(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	sync := &teamSync{client: server.Client(), url: server.URL, dir: t.TempDir()}
	if _, err := sync.once(context.Background()); err != errTeamUnavailable {
		t.Errorf("err = %v, want errTeamUnavailable", err)
	}
}

func TestTheMachineSaysWhatItIsRunningAndIsToldToRestart(t *testing.T) {
	work := t.TempDir()
	starts := filepath.Join(work, "starts")

	// The record's services, and what the machine said it was running each
	// time it asked.
	revision := int64(1)
	var said []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		said = append(said, r.Header.Get(guestapi.ServicesHeader))
		w.Header().Set("ETag", fmt.Sprintf(`"r%d"`, revision))
		if r.Header.Get("If-None-Match") == w.Header().Get("ETag") {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"files": map[string]string{},
			"services": []map[string]any{{
				"name": "api", "command": "echo started >> starts; sleep 300", "directory": work, "revision": revision,
			}},
		})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sync := &teamSync{
		client:   server.Client(),
		url:      server.URL,
		key:      "instance-id.guest-token",
		dir:      filepath.Join(t.TempDir(), "team"),
		services: newServiceManager(ctx, filepath.Join(t.TempDir(), "logs")),
	}

	changed, err := sync.once(ctx)
	if err != nil || !changed {
		t.Fatalf("first sync: changed=%v err=%v, want a service started", changed, err)
	}
	if said[0] != "{}" {
		t.Errorf("before anything was started it said %q, want an empty list: that it says anything is how the control plane knows it reports", said[0])
	}
	waitFor(t, "the service to start", func() bool { return strings.Count(contents(starts), "started") == 1 })

	// Asked again with nothing new: nothing changes, and it says what it has.
	changed, err = sync.once(ctx)
	if err != nil || changed {
		t.Fatalf("second sync: changed=%v err=%v, want nothing to have changed", changed, err)
	}
	if said[1] != `{"api":1}` {
		t.Errorf("it said it is running %q, want the service and its revision", said[1])
	}

	// The same service registered again: same command, a new revision. It is
	// started afresh, which is how changed code gets to run.
	revision = 2
	changed, err = sync.once(ctx)
	if err != nil || !changed {
		t.Fatalf("after registering again: changed=%v err=%v, want it restarted", changed, err)
	}
	waitFor(t, "the service to be started a second time", func() bool { return strings.Count(contents(starts), "started") == 2 })

	if _, err := sync.once(ctx); err != nil {
		t.Fatal(err)
	}
	if got := said[len(said)-1]; got != `{"api":2}` {
		t.Errorf("after the restart it said %q, want the new revision", got)
	}
}
