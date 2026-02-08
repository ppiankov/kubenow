package monitor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetState_ConnectionStatus_Propagated(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
		connStatus: ConnectionUnreachable,
		lastErr:    "dial tcp: connection refused",
	}

	_, _, stats := w.GetState()
	assert.Equal(t, ConnectionUnreachable, stats.Connection)
	assert.Equal(t, "dial tcp: connection refused", stats.LastError)
}

func TestGetState_ConnectionOK_NoError(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
		connStatus: ConnectionOK,
	}

	_, _, stats := w.GetState()
	assert.Equal(t, ConnectionOK, stats.Connection)
	assert.Empty(t, stats.LastError)
}

func TestGetState_EmptyProblems_NotFalseGreen(t *testing.T) {
	// When cluster is unreachable, problems list is empty but connection is bad.
	// UI must NOT show "No active problems" — it must check Connection field.
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
		connStatus: ConnectionUnreachable,
		lastErr:    "i/o timeout",
	}

	problems, _, stats := w.GetState()
	assert.Empty(t, problems)
	assert.Equal(t, ConnectionUnreachable, stats.Connection)
}

func TestSetConnectionError(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
		connStatus: ConnectionOK,
	}

	w.setConnectionError(assert.AnError)

	assert.Equal(t, ConnectionUnreachable, w.connStatus)
	assert.NotEmpty(t, w.lastErr)

	// Should have sent an update notification
	select {
	case <-w.updateChan:
		// good
	default:
		t.Fatal("expected update notification on connection error")
	}
}

func TestSetConnectionOK(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
		connStatus: ConnectionUnreachable,
		lastErr:    "connection refused",
	}

	w.setConnectionOK()

	assert.Equal(t, ConnectionOK, w.connStatus)
	assert.Empty(t, w.lastErr)

	// Should have sent an update notification (status changed)
	select {
	case <-w.updateChan:
		// good
	default:
		t.Fatal("expected update notification on connection recovery")
	}
}

func TestSetConnectionOK_NoSpuriousUpdate(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
		connStatus: ConnectionOK,
	}

	w.setConnectionOK()

	// Should NOT send update when status didn't change
	select {
	case <-w.updateChan:
		t.Fatal("unexpected update notification when status unchanged")
	default:
		// good
	}
}

func TestConnectionStatus_DefaultUnknown(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
	}

	assert.Equal(t, ConnectionUnknown, w.connStatus)
}
