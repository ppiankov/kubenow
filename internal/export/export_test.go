package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/kubenow/internal/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Format
	}{
		{"json extension", "output.json", FormatJSON},
		{"markdown extension", "output.md", FormatMarkdown},
		{"markdown full", "output.markdown", FormatMarkdown},
		{"html extension", "output.html", FormatHTML},
		{"text extension", "output.txt", FormatText},
		{"unknown extension", "output.xyz", FormatText},
		{"no extension", "output", FormatText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectFormat(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExportJSON(t *testing.T) {
	var buf bytes.Buffer
	exporter := Exporter{
		Format: FormatJSON,
		Metadata: ExportMetadata{
			GeneratedAt:    time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
			KubenowVersion: "1.2.3",
			ClusterName:    "test-cluster",
			Mode:           "default",
		},
	}

	err := exporter.Export(map[string]string{"status": "ok"}, &buf)
	require.NoError(t, err)

	var decoded JSONExport
	err = json.Unmarshal(buf.Bytes(), &decoded)
	require.NoError(t, err)
	assert.Equal(t, exporter.Metadata.Mode, decoded.Metadata.Mode)
}

func TestExportMarkdown(t *testing.T) {
	var buf bytes.Buffer
	exporter := Exporter{
		Format: FormatMarkdown,
		Metadata: ExportMetadata{
			GeneratedAt:    time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
			KubenowVersion: "1.2.3",
			Mode:           "default",
		},
	}

	resultData := &result.DefaultResult{}
	resultData.Summary.ProblemPodCount = 2
	resultData.Summary.NodeReadiness = "ready"
	resultData.Summary.ResourcePressure = "low"

	err := exporter.Export(resultData, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "# kubenow Report")
	assert.Contains(t, output, "## Cluster Summary")
}

func TestExportText(t *testing.T) {
	var buf bytes.Buffer
	exporter := Exporter{Format: FormatText}
	err := exporter.Export("plain output", &buf)
	require.NoError(t, err)
	assert.Equal(t, "plain output", buf.String())
}

func TestExportHTML(t *testing.T) {
	var buf bytes.Buffer
	exporter := Exporter{
		Format: FormatHTML,
		Metadata: ExportMetadata{
			GeneratedAt:    time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
			KubenowVersion: "1.2.3",
			Mode:           "default",
		},
	}

	err := exporter.Export(map[string]string{"status": "ok"}, &buf)
	require.NoError(t, err)
	assert.True(t, strings.Contains(buf.String(), "<!DOCTYPE html>"))
}
