package telemetry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	require.NotNil(t, m)
	require.NotNil(t, m.QueryDuration)
	require.NotNil(t, m.QueryErrors)
	require.NotNil(t, m.QueriesTotal)
	require.NotNil(t, m.AnalysisDuration)
	require.NotNil(t, m.Recommendations)
}

func TestRecordQuery(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RecordQuery("instant", 100*time.Millisecond, nil)
	m.RecordQuery("range", 200*time.Millisecond, nil)
	m.RecordQuery("instant", 50*time.Millisecond, fmt.Errorf("timeout"))

	// Gather and verify
	families, err := reg.Gather()
	require.NoError(t, err)

	found := false
	for _, f := range families {
		if f.GetName() == "kubenow_queries_total" {
			found = true
			assert.Len(t, f.GetMetric(), 2) // instant + range
		}
	}
	assert.True(t, found, "kubenow_queries_total not found")
}

func TestRecordAnalysis(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.RecordAnalysis("requests-skew", 5*time.Second)

	families, err := reg.Gather()
	require.NoError(t, err)

	found := false
	for _, f := range families {
		if f.GetName() == "kubenow_analysis_duration_seconds" {
			found = true
		}
	}
	assert.True(t, found, "kubenow_analysis_duration_seconds not found")
}

func TestServerEndpoint(t *testing.T) {
	srv := NewServer(0) // port 0 won't work for ListenAndServe, use a real port
	require.NotNil(t, srv)
	require.NotNil(t, srv.Metrics())

	// Use a high port to avoid conflicts
	port := 19091
	srv.httpServer.Addr = fmt.Sprintf(":%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = srv.Start(ctx)
	}()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	// Record some metrics
	srv.Metrics().RecordQuery("instant", 100*time.Millisecond, nil)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", port))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	bodyStr := string(body)
	assert.True(t, strings.Contains(bodyStr, "kubenow_queries_total"), "should contain kubenow_queries_total")
	assert.True(t, strings.Contains(bodyStr, "kubenow_query_duration_seconds"), "should contain kubenow_query_duration_seconds")

	cancel()
}
