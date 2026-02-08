package monitor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestView_Disconnected_NoFalseGreen(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
		connStatus: ConnectionUnreachable,
		lastErr:    "dial tcp: connection refused",
	}

	m := NewModel(w)
	m.stats.Connection = ConnectionUnreachable
	m.stats.LastError = "dial tcp: connection refused"
	m.width = 120
	m.height = 40

	view := m.View()

	assert.Contains(t, view, "Cluster unreachable")
	assert.Contains(t, view, "DISCONNECTED")
	assert.NotContains(t, view, "No active problems")
}

func TestView_Connected_Healthy(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
		connStatus: ConnectionOK,
	}

	m := NewModel(w)
	m.stats.Connection = ConnectionOK
	m.stats.TotalPods = 10
	m.stats.RunningPods = 10
	m.stats.TotalNodes = 3
	m.width = 120
	m.height = 40

	view := m.View()

	assert.Contains(t, view, "No active problems")
	assert.NotContains(t, view, "Cluster unreachable")
}

func TestView_Disconnected_ShowsError(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
		connStatus: ConnectionUnreachable,
		lastErr:    "i/o timeout",
	}

	m := NewModel(w)
	m.stats.Connection = ConnectionUnreachable
	m.stats.LastError = "i/o timeout"
	m.width = 120
	m.height = 40

	view := m.View()

	assert.Contains(t, view, "i/o timeout")
	assert.Contains(t, view, "Retrying")
}

func TestRenderDisconnected(t *testing.T) {
	m := Model{
		stats: ClusterStats{
			Connection: ConnectionUnreachable,
			LastError:  "connection refused",
		},
	}

	result := m.renderDisconnected()

	assert.True(t, strings.Contains(result, "Cluster unreachable"))
	assert.True(t, strings.Contains(result, "connection refused"))
	assert.True(t, strings.Contains(result, "Retrying"))
}

func TestHeaderStatus_Disconnected(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
	}

	m := NewModel(w)
	m.stats.Connection = ConnectionUnreachable
	m.width = 120
	m.height = 40

	view := m.View()
	assert.Contains(t, view, "DISCONNECTED")
}
