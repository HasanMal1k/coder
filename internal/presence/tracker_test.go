package presence

import (
	"testing"
	"time"
)

// later is a time comfortably after anything the test just did.
func later(d time.Duration) time.Time {
	return time.Now().Add(d)
}

func TestNobodyEverSeenIsIdleSinceStartup(t *testing.T) {
	tr := NewTracker()

	// Nothing is known about this user or machine. The clock starts when the
	// tracker did, not at the beginning of time.
	if idle := tr.IdleFor("user", "machine", later(time.Minute)); idle < 59*time.Second || idle > 61*time.Second {
		t.Errorf("idle = %v, want about a minute", idle)
	}
	if idle := tr.IdleFor("user", "machine", tr.started.Add(-time.Hour)); idle != 0 {
		t.Errorf("idle = %v for a time before startup, want 0", idle)
	}
}

func TestNotIdleWhileTheOwnerHasTheAppOpen(t *testing.T) {
	tr := NewTracker()
	done := tr.UserConnected("user")

	if idle := tr.IdleFor("user", "machine", later(time.Hour)); idle != 0 {
		t.Errorf("idle = %v while the owner is connected, want 0", idle)
	}
	// Another user's presence says nothing about this one's machines.
	if idle := tr.IdleFor("someone-else", "machine", later(time.Hour)); idle == 0 {
		t.Error("a machine counted as in use because a different user was connected")
	}

	done()
	if idle := tr.IdleFor("user", "machine", later(time.Minute)); idle < 59*time.Second || idle > 61*time.Second {
		t.Errorf("idle = %v a minute after the owner left, want about a minute", idle)
	}
}

func TestNotIdleWhileSomethingIsConnectedToTheMachine(t *testing.T) {
	tr := NewTracker()

	// The owner has closed the app, but still has the editor open in its
	// own tab.
	tr.UserConnected("user")()
	done := tr.MachineConnected("machine")

	if idle := tr.IdleFor("user", "machine", later(time.Hour)); idle != 0 {
		t.Errorf("idle = %v while the machine has a connection, want 0", idle)
	}
	// The owner's other machine has no such connection.
	if idle := tr.IdleFor("user", "other-machine", later(time.Hour)); idle == 0 {
		t.Error("a machine counted as in use because a different machine was connected")
	}

	done()
	if idle := tr.IdleFor("user", "machine", later(time.Minute)); idle == 0 {
		t.Error("still counted as in use after its last connection closed")
	}
}

func TestSeveralConnectionsAllHaveToClose(t *testing.T) {
	tr := NewTracker()

	// Two tabs.
	first := tr.UserConnected("user")
	second := tr.UserConnected("user")

	first()
	if idle := tr.IdleFor("user", "machine", later(time.Hour)); idle != 0 {
		t.Errorf("idle = %v with one tab still open, want 0", idle)
	}

	second()
	if idle := tr.IdleFor("user", "machine", later(time.Minute)); idle == 0 {
		t.Error("still counted as present after both tabs closed")
	}
}

func TestReleasingTwiceDoesNotUndercount(t *testing.T) {
	tr := NewTracker()

	first := tr.UserConnected("user")
	second := tr.UserConnected("user")
	_ = second

	// A buggy caller releasing the same connection twice must not cancel
	// out the other one.
	first()
	first()

	if idle := tr.IdleFor("user", "machine", later(time.Hour)); idle != 0 {
		t.Errorf("idle = %v with a connection still open, want 0", idle)
	}
}

func TestIdleIsMeasuredFromTheLastOneToLeave(t *testing.T) {
	tr := NewTracker()

	tr.UserConnected("user")()
	time.Sleep(30 * time.Millisecond)
	tr.MachineConnected("machine")()

	// The machine's connection closed last, a moment ago; that is when the
	// idle period began, not when the user left.
	if idle := tr.IdleFor("user", "machine", time.Now()); idle > 20*time.Millisecond {
		t.Errorf("idle = %v, want it measured from the most recent departure", idle)
	}
}

func TestReconnectingResetsIdle(t *testing.T) {
	tr := NewTracker()

	// A page reload: the connection drops and comes straight back.
	tr.UserConnected("user")()
	done := tr.UserConnected("user")
	defer done()

	if idle := tr.IdleFor("user", "machine", later(time.Hour)); idle != 0 {
		t.Errorf("idle = %v after reconnecting, want 0", idle)
	}
}
