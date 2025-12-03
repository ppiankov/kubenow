package export

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/ppiankov/kubenow/internal/snapshot"
)

// Format represents the export format type.
type Format string

const (
	FormatJSON     Format = "json"
	FormatHTML     Format = "html"
	FormatMarkdown Format = "markdown"
	FormatText     Format = "text"
)

// ExportMetadata contains metadata about the export.
type ExportMetadata struct {
	GeneratedAt    time.Time         `json:"generatedAt"`
	KubenowVersion string            `json:"kubenowVersion"`
	ClusterName    string            `json:"clusterName,omitempty"`
	Mode           string            `json:"mode"`
	Filters        snapshot.Filters  `json:"filters,omitempty"`
}

// Exporter handles exporting results in various formats.
type Exporter struct {
	Format   Format
	Metadata ExportMetadata
}

// DetectFormat detects the export format from the file extension.
func DetectFormat(path string) Format {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return FormatJSON
	case ".html", ".htm":
		return FormatHTML
	case ".md", ".markdown":
		return FormatMarkdown
	default:
		return FormatText
	}
}

// Export exports the result in the specified format.
func (e *Exporter) Export(result interface{}, w io.Writer) error {
	switch e.Format {
	case FormatJSON:
		return e.exportJSON(result, w)
	case FormatMarkdown:
		return e.exportMarkdown(result, w)
	case FormatHTML:
		return e.exportHTML(result, w)
	case FormatText:
		return e.exportText(result, w)
	default:
		return fmt.Errorf("unsupported format: %s", e.Format)
	}
}

// exportText exports in plain text format (just writes the string representation).
func (e *Exporter) exportText(result interface{}, w io.Writer) error {
	// For text format, we expect result to already be a string
	// This will be handled by the caller rendering to string first
	if str, ok := result.(string); ok {
		_, err := w.Write([]byte(str))
		return err
	}
	return fmt.Errorf("text format requires string input")
}

// exportHTML exports in HTML format (placeholder for now, will implement in html.go).
func (e *Exporter) exportHTML(result interface{}, w io.Writer) error {
	return exportHTML(result, e.Metadata, w)
}

// exportJSON exports with metadata wrapper.
func (e *Exporter) exportJSON(result interface{}, w io.Writer) error {
	return exportJSON(result, e.Metadata, w)
}

// exportMarkdown exports in Markdown format.
func (e *Exporter) exportMarkdown(result interface{}, w io.Writer) error {
	return exportMarkdown(result, e.Metadata, w)
}

// WithTimestamp adds a timestamp suffix to the filename for watch mode.
func WithTimestamp(path string, t time.Time) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	timestamp := t.Format("2006-01-02T15-04-05Z")
	return fmt.Sprintf("%s-%s%s", base, timestamp, ext)
}
