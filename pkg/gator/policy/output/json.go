package output

import (
	"encoding/json"
	"fmt"
	"io"
)

// JSONPrinter outputs results in JSON format.
type JSONPrinter struct{}

// JSONOutputVersion is the apiVersion for JSON output schema.
const JSONOutputVersion = "gator.gatekeeper.sh/v1alpha1"

// PrintPolicies outputs installed policies as JSON.
func (p *JSONPrinter) PrintPolicies(w io.Writer, policies []PolicyInfo) error {
	output := struct {
		APIVersion string       `json:"apiVersion"`
		Policies   []PolicyInfo `json:"policies"`
	}{
		APIVersion: JSONOutputVersion,
		Policies:   policies,
	}
	return p.writeJSON(w, output)
}

// PrintSearchResults outputs search results as JSON.
func (p *JSONPrinter) PrintSearchResults(w io.Writer, results []SearchResult) error {
	output := struct {
		APIVersion string         `json:"apiVersion"`
		Results    []SearchResult `json:"results"`
	}{
		APIVersion: JSONOutputVersion,
		Results:    results,
	}
	return p.writeJSON(w, output)
}

// PrintMessage outputs a message as JSON.
func (p *JSONPrinter) PrintMessage(w io.Writer, message string) error {
	output := struct {
		APIVersion string `json:"apiVersion"`
		Message    string `json:"message"`
	}{
		APIVersion: JSONOutputVersion,
		Message:    message,
	}
	return p.writeJSON(w, output)
}

// PrintInstallResult outputs install results as JSON.
func (p *JSONPrinter) PrintInstallResult(w io.Writer, result *InstallResult) error {
	output := struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Result     *InstallResult `json:"result"`
	}{
		APIVersion: JSONOutputVersion,
		Kind:       "InstallResult",
		Result:     result,
	}
	return p.writeJSON(w, output)
}

// PrintUninstallResult outputs uninstall results as JSON.
func (p *JSONPrinter) PrintUninstallResult(w io.Writer, result *UninstallResult) error {
	output := struct {
		APIVersion string           `json:"apiVersion"`
		Kind       string           `json:"kind"`
		Result     *UninstallResult `json:"result"`
	}{
		APIVersion: JSONOutputVersion,
		Kind:       "UninstallResult",
		Result:     result,
	}
	return p.writeJSON(w, output)
}

// PrintUpgradeResult outputs upgrade results as JSON.
func (p *JSONPrinter) PrintUpgradeResult(w io.Writer, result *UpgradeResult) error {
	output := struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Result     *UpgradeResult `json:"result"`
	}{
		APIVersion: JSONOutputVersion,
		Kind:       "UpgradeResult",
		Result:     result,
	}
	return p.writeJSON(w, output)
}

func (p *JSONPrinter) writeJSON(w io.Writer, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
