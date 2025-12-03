package export

import (
	"encoding/json"
	"io"
)

// JSONExport wraps the result with metadata for JSON output.
type JSONExport struct {
	Metadata ExportMetadata `json:"metadata"`
	Result   interface{}    `json:"result"`
}

// exportJSON exports the result as JSON with metadata.
func exportJSON(result interface{}, metadata ExportMetadata, w io.Writer) error {
	export := JSONExport{
		Metadata: metadata,
		Result:   result,
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(export)
}
