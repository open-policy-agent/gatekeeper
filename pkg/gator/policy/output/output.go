package output

import (
	"fmt"
	"io"
)

// Format represents the output format type.
type Format string

const (
	// FormatTable is human-readable table output.
	FormatTable Format = "table"
	// FormatJSON is JSON output.
	FormatJSON Format = "json"
)

// PolicyInfo represents installed policy information for output.
type PolicyInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Bundle      string `json:"bundle,omitempty"`
	InstalledAt string `json:"installedAt,omitempty"`
}

// SearchResult represents a policy search result for output.
type SearchResult struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// Printer defines the interface for outputting results.
type Printer interface {
	// PrintPolicies outputs a list of installed policies.
	PrintPolicies(w io.Writer, policies []PolicyInfo) error
	// PrintSearchResults outputs search results.
	PrintSearchResults(w io.Writer, results []SearchResult) error
	// PrintMessage outputs a simple message.
	PrintMessage(w io.Writer, message string) error
}

// NewPrinter creates a new Printer for the given format.
// Returns an error if the format is not recognized.
func NewPrinter(format Format) (Printer, error) {
	switch format {
	case FormatJSON:
		return &JSONPrinter{}, nil
	case FormatTable, "":
		return &TablePrinter{}, nil
	default:
		return nil, fmt.Errorf("invalid output format: %s (must be table or json)", format)
	}
}
