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
