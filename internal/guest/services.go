package guest

import (
	"context"
	"fmt"
	"log"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// serviceLogFolder is where, under the home folder, services' output is
	// kept: one file each.
	serviceLogFolder = ".coder/logs"
	// maxServiceLog is the size at which a service's log is started afresh.
	maxServiceLog = 5 << 20
)

// serviceDef is a program the control plane has asked this machine to keep
// running.
type serviceDef struct {
	Name      string            `json:"name"`
	Command   string            `json:"command"`
	Directory string            `json:"directory"`
	Port      int               `json:"port"`
	Env       map[string]string `json:"env"`
	// Revision goes up each time the control plane is asked to run the
	// service, changed or not: asking again is how it is restarted.
	Revision int64 `json:"revision"`
}

func (d serviceDef) same(other serviceDef) bool {
	return d.Revision == other.Revision && d.Command == other.Command && d.Directory == other.Directory && d.Port == other.Port && maps.Equal(d.Env, other.Env)
}

// serviceManager keeps the machine's services running: each is started,
// restarted with backoff if it exits, and stopped when it is no longer
// wanted or its definition changes. Because the list comes from the control
// plane every time the machine starts, what was running before a stop is
// running again after it.
type serviceManager struct {
	ctx    context.Context
	logDir string

	mu      sync.Mutex
	running map[string]*runningService
}

type runningService struct {
	def    serviceDef
	cancel context.CancelFunc
	done   chan struct{}
	// started is set once the program has been started at least once.
	started atomic.Bool
}

func newServiceManager(ctx context.Context, logDir string) *serviceManager {
	return &serviceManager{
		ctx:     ctx,
		logDir:  logDir,
		running: make(map[string]*runningService),
	}
}

// reconcile makes what is running match what is wanted.
//
// It reports whether anything was stopped or started.
func (m *serviceManager) reconcile(wanted []serviceDef) (changed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	byName := make(map[string]serviceDef, len(wanted))
	for _, def := range wanted {
		byName[def.Name] = def
	}

	for name, svc := range m.running {
		if def, ok := byName[name]; ok && def.same(svc.def) {
			continue
		}
		// Gone, or changed: either way this one stops. A changed one is
		// started again below, as it is now.
		svc.cancel()
		<-svc.done
		delete(m.running, name)
		changed = true
		log.Printf("service %s: stopped", name)
	}

	for name, def := range byName {
		if _, ok := m.running[name]; ok {
			continue
		}

		ctx, cancel := context.WithCancel(m.ctx)
		svc := &runningService{def: def, cancel: cancel, done: make(chan struct{})}
		m.running[name] = svc

		go func() {
			defer close(svc.done)
			m.keepRunning(ctx, def, &svc.started)
		}()
		changed = true
		log.Printf("service %s: started (%s)", name, def.Command)
	}

	return changed
}

// started returns the services whose programs have been started, by name,
// with the revision each was started for.
func (m *serviceManager) started() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make(map[string]int64, len(m.running))
	for name, svc := range m.running {
		if svc.started.Load() {
			out[name] = svc.def.Revision
		}
	}
	return out
}

// keepRunning runs a service until ctx is cancelled, starting it again
// whenever it exits.
func (m *serviceManager) keepRunning(ctx context.Context, def serviceDef, started *atomic.Bool) {
	backoff := minBackoff

	for {
		startedAt := time.Now()
		err := m.runOnce(ctx, def, started)
		if ctx.Err() != nil {
			return
		}
		log.Printf("service %s exited: %v", def.Name, err)

		if time.Since(startedAt) > stableAfter {
			backoff = minBackoff
		}

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// runOnce runs the service's command and waits for it to exit.
func (m *serviceManager) runOnce(ctx context.Context, def serviceDef, started *atomic.Bool) error {
	out, err := m.openLog(def.Name)
	if err != nil {
		return err
	}
	defer out.Close()

	fmt.Fprintf(out, "\n--- %s: starting %s in %s\n", time.Now().Format(time.RFC3339), def.Command, def.Directory)

	// Through a login shell, as the user would run it: their PATH, their
	// version managers, their shell's way of reading the command.
	cmd := exec.CommandContext(ctx, "bash", "-lc", def.Command)
	cmd.Dir = def.Directory
	cmd.Stdout = out
	cmd.Stderr = out

	cmd.Env = childEnv()
	for key, value := range def.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	// In a process group of its own, so that stopping it stops whatever it
	// started as well, and not just the shell.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = stopTimeout

	err = cmd.Start()
	if err == nil {
		started.Store(true)
		err = cmd.Wait()
	}
	if err != nil {
		// In the log too, where whoever is wondering why it isn't up looks.
		fmt.Fprintf(out, "--- %s: exited: %v\n", time.Now().Format(time.RFC3339), err)
	}
	// Whatever outlived a polite request to stop.
	if cmd.Process != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	return err
}

// openLog opens the service's log for adding to, starting it afresh if it
// has grown large.
func (m *serviceManager) openLog(name string) (*os.File, error) {
	if err := os.MkdirAll(m.logDir, 0o755); err != nil {
		return nil, fmt.Errorf("create log folder: %w", err)
	}

	path := filepath.Join(m.logDir, name+".log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(path); err == nil && info.Size() > maxServiceLog {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}

	return os.OpenFile(path, flags, 0o644)
}
