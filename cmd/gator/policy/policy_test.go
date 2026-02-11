package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInstallCommand_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError string
	}{
		{
			name:        "no args or bundle",
			args:        []string{},
			expectError: "specify policy name(s) or use --bundle",
		},
		{
			name:        "invalid output format",
			args:        []string{"test-policy", "-o", "xml"},
			expectError: "invalid output format",
		},
		{
			name:        "scoped enforcement action rejected",
			args:        []string{"test-policy", "--enforcement-action=scoped"},
			expectError: "scoped",
		},
		{
			name:        "invalid enforcement action",
			args:        []string{"test-policy", "--enforcement-action=invalid"},
			expectError: "invalid enforcement action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset package-level vars
			installBundles = nil
			installEnforcementAction = ""
			installDryRun = false
			installOutput = ""

			cmd := newInstallCommand()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectError)
		})
	}
}

func TestUninstallCommand_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError string
	}{
		{
			name:        "no args or bundle",
			args:        []string{},
			expectError: "specify policy name(s) or use --bundle",
		},
		{
			name:        "invalid output format",
			args:        []string{"test-policy", "-o", "xml"},
			expectError: "invalid output format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uninstallBundles = nil
			uninstallDryRun = false
			uninstallOutput = ""

			cmd := newUninstallCommand()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectError)
		})
	}
}

func TestUpgradeCommand_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError string
	}{
		{
			name:        "no args or all or bundle",
			args:        []string{},
			expectError: "specify policy name(s), use --bundle, or use --all",
		},
		{
			name:        "invalid enforcement action",
			args:        []string{"--all", "--enforcement-action=invalid"},
			expectError: "invalid enforcement action",
		},
		{
			name:        "invalid output format",
			args:        []string{"--all", "-o", "xml"},
			expectError: "invalid output format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upgradeAll = false
			upgradeBundles = nil
			upgradeEnforcementAction = ""
			upgradeDryRun = false
			upgradeOutput = ""

			cmd := newUpgradeCommand()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectError)
		})
	}
}

func TestUpdateCommand_ValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError string
	}{
		{
			name:        "invalid output format",
			args:        []string{"-o", "xml"},
			expectError: "invalid output format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateInsecure = false
			updateOutput = ""

			cmd := newUpdateCommand()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectError)
		})
	}
}

func TestSearchCommand_ValidationErrors(t *testing.T) {
	// search requires exactly 1 arg (cobra.ExactArgs(1))
	cmd := newSearchCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestGenerateCatalogCommand_InvalidPath(t *testing.T) {
	cmd := newGenerateCatalogCommand()
	cmd.SetArgs([]string{"--library-path=/nonexistent/path/to/library"})
	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "library")
}
