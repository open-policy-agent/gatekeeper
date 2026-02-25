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

func TestTablePrinter_PrintInstallResult(t *testing.T) {
	printer := &TablePrinter{}
	var buf bytes.Buffer

	result := &InstallResult{
		Installed: []InstallEntry{
			{Name: "policy1", Version: "v1.0.0"},
			{Name: "policy2", Version: "v2.0.0"},
		},
		Skipped:              []string{"policy3"},
		TemplatesInstalled:   2,
		ConstraintsInstalled: 1,
	}

	err := printer.PrintInstallResult(&buf, result)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "policy1")
	assert.Contains(t, output, "v1.0.0")
	assert.Contains(t, output, "installed")
	assert.Contains(t, output, "policy3")
	assert.Contains(t, output, "already installed")
	assert.Contains(t, output, "2 templates")
}

func TestTablePrinter_PrintInstallResult_DryRun(t *testing.T) {
	printer := &TablePrinter{}
	var buf bytes.Buffer

	result := &InstallResult{
		Installed: []InstallEntry{
			{Name: "policy1", Version: "v1.0.0"},
		},
		DryRun: true,
	}

	err := printer.PrintInstallResult(&buf, result)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "policy1")
	assert.Contains(t, output, "v1.0.0")
	assert.NotContains(t, output, "✓")
}

func TestTablePrinter_PrintUninstallResult(t *testing.T) {
	printer := &TablePrinter{}
	var buf bytes.Buffer

	result := &UninstallResult{
		Uninstalled: []string{"policy1"},
		NotFound:    []string{"policy2"},
		NotManaged:  []string{"policy3"},
	}

	err := printer.PrintUninstallResult(&buf, result)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "policy1")
	assert.Contains(t, output, "uninstalled")
	assert.Contains(t, output, "policy2")
	assert.Contains(t, output, "not found")
	assert.Contains(t, output, "policy3")
	assert.Contains(t, output, "not managed")
}

func TestTablePrinter_PrintUpgradeResult(t *testing.T) {
	printer := &TablePrinter{}
	var buf bytes.Buffer

	result := &UpgradeResult{
		Upgraded: []UpgradeEntry{
			{Name: "policy1", FromVersion: "v1.0.0", ToVersion: "v2.0.0"},
		},
		AlreadyCurrent: []string{"policy2"},
	}

	err := printer.PrintUpgradeResult(&buf, result)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "policy1")
	assert.Contains(t, output, "v1.0.0")
	assert.Contains(t, output, "v2.0.0")
	assert.Contains(t, output, "policy2")
	assert.Contains(t, output, "already at latest")
}

func TestJSONPrinter_PrintInstallResult(t *testing.T) {
	printer := &JSONPrinter{}
	var buf bytes.Buffer

	result := &InstallResult{
		Installed: []InstallEntry{
			{Name: "policy1", Version: "v1.0.0"},
		},
		TemplatesInstalled:   1,
		ConstraintsInstalled: 0,
	}

	err := printer.PrintInstallResult(&buf, result)
	require.NoError(t, err)

	var output struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Result     *InstallResult `json:"result"`
	}
	err = json.Unmarshal(buf.Bytes(), &output)
	require.NoError(t, err)
	assert.Equal(t, JSONOutputVersion, output.APIVersion)
	assert.Equal(t, "InstallResult", output.Kind)
	assert.Len(t, output.Result.Installed, 1)
	assert.Equal(t, "policy1", output.Result.Installed[0].Name)
}

func TestJSONPrinter_PrintUninstallResult(t *testing.T) {
	printer := &JSONPrinter{}
	var buf bytes.Buffer

	result := &UninstallResult{
		Uninstalled: []string{"policy1"},
		NotManaged:  []string{"policy2"},
	}

	err := printer.PrintUninstallResult(&buf, result)
	require.NoError(t, err)

	var output struct {
		APIVersion string           `json:"apiVersion"`
		Kind       string           `json:"kind"`
		Result     *UninstallResult `json:"result"`
	}
	err = json.Unmarshal(buf.Bytes(), &output)
	require.NoError(t, err)
	assert.Equal(t, JSONOutputVersion, output.APIVersion)
	assert.Equal(t, "UninstallResult", output.Kind)
	assert.Equal(t, []string{"policy1"}, output.Result.Uninstalled)
	assert.Equal(t, []string{"policy2"}, output.Result.NotManaged)
}

func TestJSONPrinter_PrintUpgradeResult(t *testing.T) {
	printer := &JSONPrinter{}
	var buf bytes.Buffer

	result := &UpgradeResult{
		Upgraded: []UpgradeEntry{
			{Name: "policy1", FromVersion: "v1.0.0", ToVersion: "v2.0.0"},
		},
	}

	err := printer.PrintUpgradeResult(&buf, result)
	require.NoError(t, err)

	var output struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Result     *UpgradeResult `json:"result"`
	}
	err = json.Unmarshal(buf.Bytes(), &output)
	require.NoError(t, err)
	assert.Equal(t, JSONOutputVersion, output.APIVersion)
	assert.Equal(t, "UpgradeResult", output.Kind)
	assert.Len(t, output.Result.Upgraded, 1)
	assert.Equal(t, "v2.0.0", output.Result.Upgraded[0].ToVersion)
}
