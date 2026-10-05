package guest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitFor waits for cond to hold, failing the test if it doesn't soon.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func contents(path string) string {
	raw, _ := os.ReadFile(path)
	return string(raw)
}

func TestServicesAreKeptRunning(t *testing.T) {
	work := t.TempDir()
	logs := filepath.Join(t.TempDir(), "logs")
	marks := filepath.Join(work, "starts")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newServiceManager(ctx, logs)

	// Says it has started, says what address it was given, then exits: a
	// program that keeps falling over.
	crashing := serviceDef{
		Name:      "api",
		Command:   `echo "started with $API_URL" >> starts; echo to the log; exit 3`,
		Directory: work,
		Env:       map[string]string{"API_URL": "http://api:8000"},
	}
	m.reconcile([]serviceDef{crashing})

	waitFor(t, "the service to be started again after it exited", func() bool {
		return strings.Count(contents(marks), "started") >= 2
	})
	if !strings.Contains(contents(marks), "started with http://api:8000") {
		t.Errorf("the service was not given its connections: %q", contents(marks))
	}

	log := contents(filepath.Join(logs, "api.log"))
	if !strings.Contains(log, "to the log") || !strings.Contains(log, "exited: exit status 3") {
		t.Errorf("the log does not hold its output and how it ended:\n%s", log)
	}
}

func TestServicesFollowWhatIsWanted(t *testing.T) {
	work := t.TempDir()
	pidfile := filepath.Join(work, "pid")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newServiceManager(ctx, filepath.Join(t.TempDir(), "logs"))

	running := func() string { return strings.TrimSpace(contents(pidfile)) }
	alive := func(pid string) bool {
		return pid != "" && exists(filepath.Join("/proc", pid))
	}

	def := serviceDef{Name: "web", Command: `echo $$ > pid; echo one > which; exec sleep 300`, Directory: work}
	m.reconcile([]serviceDef{def})
	waitFor(t, "the service to start", func() bool { return alive(running()) })
	first := running()

	// Asked for again, unchanged: left alone.
	m.reconcile([]serviceDef{def})
	time.Sleep(100 * time.Millisecond)
	if running() != first || !alive(first) {
		t.Errorf("an unchanged service was restarted (pid %s, then %s)", first, running())
	}

	// Changed: the old one is stopped and the new one started.
	def.Command = `echo $$ > pid; echo two > which; exec sleep 300`
	m.reconcile([]serviceDef{def})
	waitFor(t, "the changed service to replace the old", func() bool {
		return running() != first && alive(running()) && strings.TrimSpace(contents(filepath.Join(work, "which"))) == "two"
	})
	if alive(first) {
		t.Error("the old process is still running after its service changed")
	}
	second := running()

	// No longer wanted: stopped.
	m.reconcile(nil)
	waitFor(t, "the removed service to stop", func() bool { return !alive(second) })
}

func TestStoppingAServiceStopsWhatItStarted(t *testing.T) {
	work := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newServiceManager(ctx, filepath.Join(t.TempDir(), "logs"))

	// A shell that starts a child and waits: the usual shape of "npm start".
	m.reconcile([]serviceDef{{Name: "dev", Command: `sleep 300 & echo $! > child; wait`, Directory: work}})

	child := func() string { return strings.TrimSpace(contents(filepath.Join(work, "child"))) }
	waitFor(t, "the child to start", func() bool { return child() != "" && exists(filepath.Join("/proc", child())) })
	pid := child()

	m.reconcile(nil)
	waitFor(t, "the child to be stopped with its service", func() bool { return !exists(filepath.Join("/proc", pid)) })
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
