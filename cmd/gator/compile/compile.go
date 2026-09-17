package compile

import (
	"fmt"

	cmdutils "github.com/open-policy-agent/gatekeeper/v3/cmd/gator/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/compile"
	"github.com/spf13/cobra"
)

const (
	examples = `# compile a gatekeeper-library style policy directory
gator compile --source-dir=src/general/requiredlabels

# set the repo root explicitly (needed if the source dir is not under src/)
gator compile --source-dir=src/general/requiredlabels --working-dir=.

# write compiled output to a file
gator compile --source-dir=src/general/requiredlabels --output=library/general/requiredlabels/template.yaml`
)

var Cmd = &cobra.Command{
	Use:     "compile",
	Short:   "compile renders ConstraintTemplates from gatekeeper-library constraint.tmpl sources",
	Example: examples,
	Run:     run,
	Args:    cobra.ExactArgs(0),
}

var (
	flagSourceDir  string
	flagWorkingDir string
	flagOutput     string
)

const (
	flagNameSourceDir  = "source-dir"
	flagNameWorkingDir = "working-dir"
	flagNameOutput     = "output"
)

func init() {
	Cmd.Flags().StringVar(&flagSourceDir, flagNameSourceDir, "", "Policy source directory containing constraint.tmpl (gatekeeper-library layout).")
	Cmd.Flags().StringVar(&flagWorkingDir, flagNameWorkingDir, "", "Repository root used to resolve file.Read paths in constraint.tmpl. Inferred when omitted.")
	Cmd.Flags().StringVarP(&flagOutput, flagNameOutput, "o", "", "Output file path. Prints to stdout when omitted.")
}

func run(_ *cobra.Command, _ []string) {
	if flagSourceDir == "" {
		cmdutils.ErrFatalf("--%s must be specified", flagNameSourceDir)
	}

	output, err := compile.Compile(compile.Options{
		SourceDir:  flagSourceDir,
		WorkingDir: flagWorkingDir,
	})
	if err != nil {
		cmdutils.ErrFatalf("compiling: %v", err)
	}

	if flagOutput == "" {
		fmt.Print(output)
		return
	}

	cmdutils.WriteToFile(output, flagOutput)
}
