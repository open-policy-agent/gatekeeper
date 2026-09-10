package policy

import (
	"errors"
	"testing"

	gatorpolicy "github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/policy/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			name:        "scoped enforcement action rejected",
			args:        []string{"test-policy", "--enforcement-action=scoped"},
			expectError: "scoped",
		},
		{
			name:        "invalid enforcement action",
			args:        []string{"test-policy", "--enforcement-action=invalid"},
			expectError: "invalid enforcement action",
		},
		{
			name:        "invalid output format",
			args:        []string{"test-policy", "-o", "xml"},
			expectError: "invalid output format",
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

func TestInstallExitError(t *testing.T) {
	unknownEntry := client.IncompatibleEntry{Name: "p1", Reason: "cluster Kubernetes version was not queried in this offline dry-run preview"}

	t.Run("offline dry-run with only unknown compatibility exits 0", func(t *testing.T) {
		// An offline dry-run cannot determine compatibility, so a bounded policy
		// lands in Unknown. That is the expected preview outcome and must not fail
		// the command, or scripts gating on dry-run success break.
		result := &client.InstallResult{
			Unknown:        []client.IncompatibleEntry{unknownEntry},
			TotalRequested: 1,
		}
		err, hint := installExitError(result, true /* dryRun */, nil)
		assert.NoError(t, err)
		assert.Empty(t, hint)
	})

	t.Run("real run with unknown compatibility signals partial success", func(t *testing.T) {
		// Outside a dry-run the cluster version is resolved, so Unknown should not
		// normally occur; if it does, it is a genuine skip and must not exit 0.
		result := &client.InstallResult{
			Unknown:        []client.IncompatibleEntry{unknownEntry},
			TotalRequested: 1,
		}
		err, _ := installExitError(result, false /* dryRun */, nil)
		var exitErr *gatorpolicy.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Equal(t, gatorpolicy.ExitPartialSuccess, exitErr.Code)
	})

	t.Run("incompatible policies signal partial success even in a dry-run", func(t *testing.T) {
		// A confirmed incompatibility (e.g. a dry-run with a pre-resolved version)
		// is a real skip, unlike unknown compatibility, so it still fails.
		result := &client.InstallResult{
			Incompatible:   []client.IncompatibleEntry{{Name: "p1", Reason: "out of range"}},
			TotalRequested: 1,
		}
		err, _ := installExitError(result, true /* dryRun */, nil)
		var exitErr *gatorpolicy.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Equal(t, gatorpolicy.ExitPartialSuccess, exitErr.Code)
	})

	t.Run("failed policies signal partial success with a re-run hint", func(t *testing.T) {
		result := &client.InstallResult{
			Installed:      []string{"good"},
			Failed:         []string{"bad"},
			Errors:         map[string]string{"bad": "boom"},
			TotalRequested: 2,
		}
		err, hint := installExitError(result, false, nil)
		var exitErr *gatorpolicy.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Equal(t, gatorpolicy.ExitPartialSuccess, exitErr.Code)
		assert.Contains(t, hint, "Re-run command")
	})

	t.Run("version resolution error with nothing installed is a cluster error", func(t *testing.T) {
		result := &client.InstallResult{
			Failed:         []string{"bad"},
			Errors:         map[string]string{"bad": "unreachable"},
			TotalRequested: 1,
		}
		err, _ := installExitError(result, false, errors.New("determining cluster Kubernetes version"))
		var exitErr *gatorpolicy.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Equal(t, gatorpolicy.ExitClusterError, exitErr.Code)
	})

	t.Run("full success exits 0", func(t *testing.T) {
		result := &client.InstallResult{
			Installed:      []string{"good"},
			TotalRequested: 1,
		}
		err, hint := installExitError(result, false, nil)
		assert.NoError(t, err)
		assert.Empty(t, hint)
	})
}

func TestGenerateCatalogCommand_InvalidPath(t *testing.T) {
	cmd := newGenerateCatalogCommand()
	cmd.SetArgs([]string{"--library-path=/nonexistent/path/to/library"})
	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "library")
}
