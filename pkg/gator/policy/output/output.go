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

// InstallResult represents the result of an install operation for output.
type InstallResult struct {
	Installed            []InstallEntry `json:"installed,omitempty"`
	Skipped              []string       `json:"skipped,omitempty"`
	Failed               []FailedEntry  `json:"failed,omitempty"`
	TemplatesInstalled   int            `json:"templatesInstalled"`
	ConstraintsInstalled int            `json:"constraintsInstalled"`
	DryRun               bool           `json:"dryRun"`
}

// InstallEntry represents a single installed policy.
type InstallEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// FailedEntry represents a policy that failed an operation.
type FailedEntry struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// UninstallResult represents the result of an uninstall operation for output.
type UninstallResult struct {
	Uninstalled []string      `json:"uninstalled,omitempty"`
	NotFound    []string      `json:"notFound,omitempty"`
	NotManaged  []string      `json:"notManaged,omitempty"`
	Failed      []FailedEntry `json:"failed,omitempty"`
	DryRun      bool          `json:"dryRun"`
}

// UpgradeResult represents the result of an upgrade operation for output.
type UpgradeResult struct {
	Upgraded       []UpgradeEntry `json:"upgraded,omitempty"`
	AlreadyCurrent []string       `json:"alreadyCurrent,omitempty"`
	NotInstalled   []string       `json:"notInstalled,omitempty"`
	NotFound       []string       `json:"notFound,omitempty"`
	Failed         []FailedEntry  `json:"failed,omitempty"`
	DryRun         bool           `json:"dryRun"`
}

// UpgradeEntry represents a single upgraded policy with version change.
type UpgradeEntry struct {
	Name        string `json:"name"`
	FromVersion string `json:"fromVersion"`
	ToVersion   string `json:"toVersion"`
}

// UpdateResult represents the result of a catalog update operation for output.
type UpdateResult struct {
	CatalogVersion string         `json:"catalogVersion"`
	PolicyCount    int            `json:"policyCount"`
	BundleCount    int            `json:"bundleCount"`
	Upgradable     []UpgradeEntry `json:"upgradable,omitempty"`
}

// Printer defines the interface for outputting results.
type Printer interface {
	// PrintPolicies outputs a list of installed policies.
	PrintPolicies(w io.Writer, policies []PolicyInfo) error
	// PrintSearchResults outputs search results.
	PrintSearchResults(w io.Writer, results []SearchResult) error
	// PrintInstallResult outputs the result of an install operation.
	PrintInstallResult(w io.Writer, result *InstallResult) error
	// PrintUninstallResult outputs the result of an uninstall operation.
	PrintUninstallResult(w io.Writer, result *UninstallResult) error
	// PrintUpgradeResult outputs the result of an upgrade operation.
	PrintUpgradeResult(w io.Writer, result *UpgradeResult) error
	// PrintUpdateResult outputs the result of a catalog update operation.
	PrintUpdateResult(w io.Writer, result *UpdateResult) error
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
