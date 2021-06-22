package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/open-policy-agent/gatekeeper/pkg/gktest"
	"github.com/spf13/cobra"
)

var run string

func init() {
	rootCmd.Flags().StringVarP(&run, "run", "r", "",
		`regular expression which filters tests to run by name`)
}

var rootCmd = &cobra.Command{
	Use:   "gatekeeper-test-alpha path [--run=name]",
	Short: "Gatekeeper Test Alpha is a unit test CLI for Gatekeeper Constraints",
	Example: `  # Run all tests in label-tests.yaml
  gatekeeper-test-alpha label-tests.yaml

  # Run all suites whose names contain "forbid-labels".
  gatekeeper-test-alpha tests/... --run forbid-labels//

  # Run all tests whose names contain "nginx-deployment".
  gatekeeper-test-alpha tests/... --run //nginx-deployment

  # Run all tests whose names exactly match "nginx-deployment".
  gatekeeper-test-alpha tests/... --run '//^nginx-deployment$'

  # Run all tests that are either named "forbid-labels" or are
  # in suites named "forbid-labels".
  gatekeeper-test-alpha tests/... --run '^forbid-labels$'`,
	Version: "alpha",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]

		testFiles, err := gktest.ToTestFiles(path)
		if err != nil {
			return fmt.Errorf("listing test files: %w", err)
		}
		filter, err := gktest.NewFilter(run)
		if err != nil {
			return fmt.Errorf("compiling filter: %w", err)
		}

		isFailure := false
		for _, f := range testFiles {
			result := gktest.Run(f, filter)
			// If Result contains an error status, it is safe to execute tests in other
			// files so we can continue execution.
			isFailure = isFailure || result.IsFailure()
			fmt.Println(result.String())
		}
		if isFailure {
			// At least one test failed or there was a problem executing tests in at
			// least one file.
			return errors.New("FAIL")
		}

		return nil
	},
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
