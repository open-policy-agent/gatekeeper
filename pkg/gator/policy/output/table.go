package output

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// TablePrinter outputs results in human-readable table format.
type TablePrinter struct{}

// PrintPolicies outputs a table of installed policies.
func (p *TablePrinter) PrintPolicies(w io.Writer, policies []PolicyInfo) error {
	if len(policies) == 0 {
		_, err := fmt.Fprintln(w, "No policies installed.")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	// Header
	fmt.Fprintln(tw, "NAME\tVERSION\tBUNDLE\tINSTALLED")

	// Rows
	for _, pol := range policies {
		bundle := pol.Bundle
		if bundle == "" {
			bundle = "-"
		}
		installedAt := pol.InstalledAt
		if len(installedAt) > 10 {
			installedAt = installedAt[:10] // Just the date part
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", pol.Name, pol.Version, bundle, installedAt)
	}

	return nil
}

// PrintSearchResults outputs a table of search results.
func (p *TablePrinter) PrintSearchResults(w io.Writer, results []SearchResult) error {
	if len(results) == 0 {
		_, err := fmt.Fprintln(w, "No policies found.")
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	// Header
	fmt.Fprintln(tw, "NAME\tVERSION\tCATEGORY\tDESCRIPTION")

	// Rows
	for _, r := range results {
		desc := r.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Name, r.Version, r.Category, desc)
	}

	return nil
}

// PrintMessage outputs a simple message.
func (p *TablePrinter) PrintMessage(w io.Writer, message string) error {
	_, err := fmt.Fprintln(w, message)
	return err
}

// PrintInstallResult outputs install results as a human-readable table.
func (p *TablePrinter) PrintInstallResult(w io.Writer, result *InstallResult) error {
	prefix := ""
	if result.DryRun {
		prefix = "(dry-run) "
	}

	for _, entry := range result.Installed {
		if result.DryRun {
			fmt.Fprintf(w, "%s%s %s\n", prefix, entry.Name, entry.Version)
		} else {
			fmt.Fprintf(w, "✓ %s (%s) installed\n", entry.Name, entry.Version)
		}
	}

	for _, name := range result.Skipped {
		fmt.Fprintf(w, "- %s (already installed at same version)\n", name)
	}

	for _, f := range result.Failed {
		fmt.Fprintf(w, "✗ %s - failed: %s\n", f.Name, f.Error)
	}

	if len(result.Failed) == 0 && !result.DryRun && result.TemplatesInstalled > 0 {
		fmt.Fprintf(w, "\n✓ Installed %d templates, %d constraints\n",
			result.TemplatesInstalled, result.ConstraintsInstalled)
	}

	return nil
}

// PrintUninstallResult outputs uninstall results as a human-readable table.
func (p *TablePrinter) PrintUninstallResult(w io.Writer, result *UninstallResult) error {
	for _, name := range result.Uninstalled {
		if result.DryRun {
			fmt.Fprintf(w, "%s\n", name)
		} else {
			fmt.Fprintf(w, "✓ %s uninstalled\n", name)
		}
	}

	for _, name := range result.NotFound {
		fmt.Fprintf(w, "- %s (not found)\n", name)
	}

	for _, name := range result.NotManaged {
		fmt.Fprintf(w, "✗ %s - not managed by gator\n", name)
	}

	for _, f := range result.Failed {
		fmt.Fprintf(w, "✗ %s - failed: %s\n", f.Name, f.Error)
	}

	return nil
}

// PrintUpgradeResult outputs upgrade results as a human-readable table.
func (p *TablePrinter) PrintUpgradeResult(w io.Writer, result *UpgradeResult) error {
	if result.DryRun && len(result.Upgraded) > 0 {
		fmt.Fprintf(w, "==> Would upgrade %d policies:\n", len(result.Upgraded))
	}

	for _, change := range result.Upgraded {
		if result.DryRun {
			fmt.Fprintf(w, "%s %s -> %s\n", change.Name, change.FromVersion, change.ToVersion)
		} else {
			fmt.Fprintf(w, "✓ %s upgraded (%s → %s)\n", change.Name, change.FromVersion, change.ToVersion)
		}
	}

	for _, name := range result.AlreadyCurrent {
		fmt.Fprintf(w, "- %s (already at latest version)\n", name)
	}

	for _, name := range result.NotInstalled {
		fmt.Fprintf(w, "- %s (not installed)\n", name)
	}

	for _, name := range result.NotFound {
		fmt.Fprintf(w, "- %s (not found in catalog)\n", name)
	}

	for _, f := range result.Failed {
		fmt.Fprintf(w, "✗ %s - failed: %s\n", f.Name, f.Error)
	}

	if len(result.Upgraded) == 0 && len(result.Failed) == 0 {
		if len(result.NotFound) > 0 {
			fmt.Fprintf(w, "\nNo upgrades available. %d policies not found in catalog.\n", len(result.NotFound))
		} else {
			fmt.Fprintln(w, "\nAll policies are up to date.")
		}
	}

	return nil
}
