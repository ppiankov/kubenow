package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRestartDelta_WithBaseline(t *testing.T) {
	m := &LatchMonitor{
		restartBaseline: map[string]int32{
			"ns/pod-a/app": 5,
		},
	}

	// 5 restarts existed at baseline, now 8 → delta is 3
	delta := m.restartDelta("ns", "pod-a", "app", 8)
	assert.Equal(t, int32(3), delta)
}

func TestRestartDelta_NoBaseline(t *testing.T) {
	m := &LatchMonitor{
		restartBaseline: map[string]int32{},
	}

	// No baseline recorded → falls back to full count
	delta := m.restartDelta("ns", "pod-b", "app", 6)
	assert.Equal(t, int32(6), delta)
}

func TestRestartDelta_ZeroDelta(t *testing.T) {
	m := &LatchMonitor{
		restartBaseline: map[string]int32{
			"ns/pod-c/app": 3,
		},
	}

	// Same count as baseline → no restarts during latch
	delta := m.restartDelta("ns", "pod-c", "app", 3)
	assert.Equal(t, int32(0), delta)
}

func TestRestartDelta_PodRecreated(t *testing.T) {
	m := &LatchMonitor{
		restartBaseline: map[string]int32{
			"ns/pod-d/app": 10,
		},
	}

	// New pod has fewer restarts than baseline → pod was recreated
	delta := m.restartDelta("ns", "pod-d", "app", 2)
	assert.Equal(t, int32(2), delta)
}

func TestRestartDelta_NilBaseline(t *testing.T) {
	m := &LatchMonitor{
		restartBaseline: nil,
	}

	// Nil map → falls back to full count
	delta := m.restartDelta("ns", "pod-e", "app", 4)
	assert.Equal(t, int32(4), delta)
}
