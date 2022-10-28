package verify

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/open-policy-agent/gatekeeper/pkg/gator"
	"github.com/spf13/cobra"
)

const (
	examples = `  # Run all tests in label-tests.yaml
  gator verify label-tests.yaml

  # Run all tests whose names contain "forbid-labels".
  gator verify tests/... --run forbid-labels//

  # Run all cases whose names contain "nginx-deployment".
  gator verify tests/... --run //nginx-deployment

  # Run all cases whose names exactly match "nginx-deployment".
  gator verify tests/... --run '//^nginx-deployment$'

  # Run all cases that are either named "forbid-labels" or are
  # in tests named "forbid-labels".
  gator verify tests/... --run '^forbid-labels$'`
)

var (
	run          string
	verbose      bool
	includeTrace bool
)

func init() {
	Cmd.Flags().StringVarP(&run, "run", "r", "",
		`regular expression which filters tests to run by name`)
	Cmd.Flags().BoolVarP(&verbose, "verbose", "v", false,
		`print extended test output`)
	Cmd.Flags().BoolVarP(&includeTrace, "trace", "t", false,
		`include a trace for the underlying constraint framework evaluation`)
}

// Cmd is the gator verify subcommand.
var Cmd = &cobra.Command{
	Use:     "verify path [--run=name]",
	Short:   "verify suites of tests on Gatekeeper Constraints",
	Example: examples,
	Args:    cobra.ExactArgs(1),
	RunE:    runE,
}

func runE(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	originalPath := args[0]

	targetPath := originalPath

	// Convert path to be absolute. Allowing for relative and absolute paths
	// everywhere in the code leads to unnecessary complexity, so the first
	// thing we do on encountering a path is to convert it to an absolute path.
	var err error
	if !filepath.IsAbs(targetPath) {
		targetPath, err = filepath.Abs(targetPath)
		if err != nil {
			return fmt.Errorf("getting absolute path: %w", err)
		}
	}

	// Create the base file system. We use fs.FS rather than direct calls to
	// os.ReadFile or filepath.WalkDir to make testing easier and keep logic
	// os-independent.
	fileSystem := getFS(targetPath)

	// fs.FS does not allow the drive prefix for absolute Windows paths.
	if runtime.GOOS == "windows" {
		targetPath = targetPath[2:]
		targetPath = filepath.ToSlash(targetPath)
	}

	recursive := false
	// filepath.Abs strips "/..." from the end of Windows paths, so check the original string.
	if strings.HasSuffix(originalPath, "/...") {
		recursive = true
		targetPath = strings.TrimSuffix(targetPath, "...")
	}
	targetPath = strings.Trim(targetPath, "/")

	suites, err := gator.ReadSuites(fileSystem, targetPath, recursive)
	if err != nil {
		return fmt.Errorf("listing test files: %w", err)
	}
	filter, err := gator.NewFilter(run)
	if err != nil {
		return fmt.Errorf("compiling filter: %w", err)
	}

	return runSuites(cmd.Context(), fileSystem, suites, filter)
}

func runSuites(ctx context.Context, fileSystem fs.FS, suites []*gator.Suite, filter gator.Filter) error {
	isFailure := false

	runner, err := gator.NewRunner(fileSystem, gator.NewOPAClient, gator.WithIncludeTrace(includeTrace))
	if err != nil {
		return err
	}

	results := make([]gator.SuiteResult, len(suites))
	i := 0

	for _, suite := range suites {
		suiteResult := runner.Run(ctx, filter, suite)
		for _, testResult := range suiteResult.TestResults {
			if testResult.Error != nil {
				isFailure = true
			}
			for _, caseResult := range testResult.CaseResults {
				if caseResult.Error != nil {
					isFailure = true
				}
			}
		}

		results[i] = suiteResult
		i++
	}
	w := &strings.Builder{}
	printer := gator.PrinterGo{}
	err = printer.Print(w, results, verbose)
	if err != nil {
		return err
	}
	fmt.Println(w)

	if isFailure {
		// At least one test failed or there was a problem executing tests in at
		// least one file.
		return errors.New("FAIL")
	}
	return nil
}

func getFS(path string) fs.FS {
	// TODO(#1397): Check that this produces the correct file system string on
	//  Windows. We may need to add a trailing `/` for fs.filesystem to function properly.
	root := filepath.VolumeName(path)
	if root == "" {
		// We are running on a unix-like filesystem without volume names, so the
		// file system root is `/`.
		root = "/"
	}

	return os.DirFS(root)
}
