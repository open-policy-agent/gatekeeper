package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTablePrinter_PrintPolicies(t *testing.T) {
	printer := &TablePrinter{}
	var buf bytes.Buffer

	policies := []PolicyInfo{
		{Name: "policy1", Version: "v1.0.0", Bundle: "test-bundle", InstalledAt: "2026-01-08T10:30:00Z"},
		{Name: "policy2", Version: "v2.0.0", Bundle: "", InstalledAt: "2026-01-07T09:00:00Z"},
	}

	err := printer.PrintPolicies(&buf, policies)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "VERSION")
	assert.Contains(t, output, "BUNDLE")
	assert.Contains(t, output, "INSTALLED")
	assert.Contains(t, output, "policy1")
	assert.Contains(t, output, "v1.0.0")
	assert.Contains(t, output, "test-bundle")
	assert.Contains(t, output, "policy2")
	assert.Contains(t, output, "-") // For empty bundle
}

func TestTablePrinter_PrintPolicies_Empty(t *testing.T) {
	printer := &TablePrinter{}
	var buf bytes.Buffer

	err := printer.PrintPolicies(&buf, []PolicyInfo{})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "No policies installed")
}

func TestTablePrinter_PrintSearchResults(t *testing.T) {
	printer := &TablePrinter{}
	var buf bytes.Buffer

	results := []SearchResult{
		{Name: "k8srequiredlabels", Version: "v1.2.0", Category: "general", Description: "Requires labels"},
		{Name: "k8scontainerlimits", Version: "v1.0.0", Category: "general", Description: "Requires resource limits"},
	}

	err := printer.PrintSearchResults(&buf, results)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "VERSION")
	assert.Contains(t, output, "CATEGORY")
	assert.Contains(t, output, "DESCRIPTION")
	assert.Contains(t, output, "k8srequiredlabels")
	assert.Contains(t, output, "general")
}

func TestTablePrinter_PrintSearchResults_TruncatesLongDescription(t *testing.T) {
	printer := &TablePrinter{}
	var buf bytes.Buffer

	longDesc := strings.Repeat("a", 100)
	results := []SearchResult{
		{Name: "policy", Version: "v1.0.0", Category: "general", Description: longDesc},
	}

	err := printer.PrintSearchResults(&buf, results)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "...")
	assert.NotContains(t, output, longDesc)
}

func TestJSONPrinter_PrintPolicies(t *testing.T) {
	printer := &JSONPrinter{}
	var buf bytes.Buffer

	policies := []PolicyInfo{
		{Name: "policy1", Version: "v1.0.0", Bundle: "test-bundle", InstalledAt: "2026-01-08T10:30:00Z"},
	}

	err := printer.PrintPolicies(&buf, policies)
	require.NoError(t, err)

	// Verify JSON is valid and contains apiVersion
	var result struct {
		APIVersion string       `json:"apiVersion"`
		Policies   []PolicyInfo `json:"policies"`
	}
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, JSONOutputVersion, result.APIVersion)
	assert.Len(t, result.Policies, 1)
	assert.Equal(t, "policy1", result.Policies[0].Name)
}

func TestJSONPrinter_PrintSearchResults(t *testing.T) {
	printer := &JSONPrinter{}
	var buf bytes.Buffer

	results := []SearchResult{
		{Name: "k8srequiredlabels", Version: "v1.2.0", Category: "general", Description: "Requires labels"},
	}

	err := printer.PrintSearchResults(&buf, results)
	require.NoError(t, err)

	// Verify JSON is valid and contains apiVersion
	var output struct {
		APIVersion string         `json:"apiVersion"`
		Results    []SearchResult `json:"results"`
	}
	err = json.Unmarshal(buf.Bytes(), &output)
	require.NoError(t, err)
	assert.Equal(t, JSONOutputVersion, output.APIVersion)
	assert.Len(t, output.Results, 1)
	assert.Equal(t, "k8srequiredlabels", output.Results[0].Name)
}

func TestNewPrinter(t *testing.T) {
	tablePrinter, err := NewPrinter(FormatTable)
	require.NoError(t, err)
	assert.IsType(t, &TablePrinter{}, tablePrinter)

	jsonPrinter, err := NewPrinter(FormatJSON)
	require.NoError(t, err)
	assert.IsType(t, &JSONPrinter{}, jsonPrinter)

	// Default (empty string) should be table
	defaultPrinter, err := NewPrinter("")
	require.NoError(t, err)
	assert.IsType(t, &TablePrinter{}, defaultPrinter)

	// Invalid format should return error
	_, err = NewPrinter("invalid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid output format")
}
