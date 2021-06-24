package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

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

		// Convert path to be absolute. Allowing for relative and absolute paths
		// everywhere in the code leads to unnecessary complexity, so the first
		// thing we do on encountering a path is to convert it to an absolute path.
		var err error
		if !filepath.IsAbs(path) {
			path, err = filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("getting absolute path: %w", err)
			}
		}

		// Create the base file system. We use fs.FS rather than direct calls to
		// os.ReadFile or filepath.WalkDir to make testing easier and keep logic
		// os-independent.
		fileSystem := getFS(path)

		suites, err := gktest.ReadSuites(fileSystem, path)
		if err != nil {
			return fmt.Errorf("listing test files: %w", err)
		}
		filter, err := gktest.NewFilter(run)
		if err != nil {
			return fmt.Errorf("compiling filter: %w", err)
		}

		isFailure := false
		for _, s := range suites {
			if !filter.MatchesSuite(s) {
				continue
			}

			results := s.Run(fileSystem, filter)
			for _, result := range results {
				if result.IsFailure() {
					isFailure = true
				}
				fmt.Println(result.String())
			}
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

func getFS(path string) fs.FS {
	// TODO(#1397): Check that this produces the correct file system string on
	//  Windows. We may need to add a trailing `/` for fs.FS to function properly.
	root := filepath.VolumeName(path)
	if root == "" {
		// We are running on a unix-like filesystem without volume names, so the
		// file system root is `/`.
		root = "/"
	}

	return os.DirFS(root)
}
