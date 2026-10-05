// Package presence keeps track of whether anyone is using a machine: whether
// its owner has the app open, and whether a browser is connected to the
// machine itself (its terminal, its editor). It answers one question, "how
// long has nobody been here?", which is what stopping idle machines is based
// on.
//
// Everything is held in memory, so it describes this process only.
package presence

import (
	"sync"
	"time"
)

type Tracker struct {
	mu sync.Mutex
	// started stands in for "last seen" for anything we have no record of. A
	// freshly started server knows nothing about who was around a moment
	// ago, so every machine gets a full grace period from here rather than
	// being judged idle since the beginning of time.
	started  time.Time
	users    map[string]*entry
	machines map[string]*entry
}

// entry is the state of one user or one machine.
type entry struct {
	// open is how many connections are up right now.
	open int
	// lastClosed is when open last dropped to zero.
	lastClosed time.Time
}

func NewTracker() *Tracker {
	return &Tracker{
		started:  time.Now(),
		users:    make(map[string]*entry),
		machines: make(map[string]*entry),
	}
}

// UserConnected records that userID has the app open. The caller must call
// the returned function, once, when that connection ends.
func (t *Tracker) UserConnected(userID string) (done func()) {
	return t.connect(t.users, userID)
}

// MachineConnected records that a browser is connected to instanceID. The
// caller must call the returned function, once, when that connection ends.
func (t *Tracker) MachineConnected(instanceID string) (done func()) {
	return t.connect(t.machines, instanceID)
}

// IdleFor reports how long a machine has had nobody: neither its owner with
// the app open, nor anything connected to the machine. It is zero while
// either is true.
func (t *Tracker) IdleFor(userID, instanceID string, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	last := t.started
	for _, e := range []*entry{t.users[userID], t.machines[instanceID]} {
		if e == nil {
			continue
		}
		if e.open > 0 {
			return 0
		}
		if e.lastClosed.After(last) {
			last = e.lastClosed
		}
	}

	if idle := now.Sub(last); idle > 0 {
		return idle
	}
	return 0
}

func (t *Tracker) connect(entries map[string]*entry, key string) func() {
	t.mu.Lock()
	e := entries[key]
	if e == nil {
		e = &entry{}
		entries[key] = e
	}
	e.open++
	t.mu.Unlock()

	var once sync.Once
	return func() {
		// Guarded, so a caller that releases twice can't make something
		// that is still in use look abandoned.
		once.Do(func() {
			t.mu.Lock()
			defer t.mu.Unlock()

			e.open--
			if e.open == 0 {
				e.lastClosed = time.Now()
			}
		})
	}
}
