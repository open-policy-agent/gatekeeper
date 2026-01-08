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

func (p *JSONPrinter) writeJSON(w io.Writer, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
