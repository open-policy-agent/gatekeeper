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

# compile a constraint.tmpl with an explicit repository root
gator compile --filename=src/general/requiredlabels/constraint.tmpl --working-dir=.

# inject Rego into a template scaffold
gator compile --filename=template.yaml --rego=src.rego

# write compiled output to a file
gator compile --source-dir=src/general/requiredlabels --output=library/general/requiredlabels/template.yaml`
)

var Cmd = &cobra.Command{
	Use:     "compile",
	Short:   "compile renders ConstraintTemplates from constraint.tmpl and separate Rego/CEL source files",
	Example: examples,
	Run:     run,
	Args:    cobra.ExactArgs(0),
}

var (
	flagFilename   string
	flagSourceDir  string
	flagWorkingDir string
	flagRegoPaths  []string
	flagCelPath    string
	flagOutput     string
)

const (
	flagNameFilename   = "filename"
	flagNameSourceDir  = "source-dir"
	flagNameWorkingDir = "working-dir"
	flagNameRego       = "rego"
	flagNameCel        = "cel"
	flagNameOutput     = "output"
)

func init() {
	Cmd.Flags().StringVarP(&flagFilename, flagNameFilename, "f", "", "Path to a constraint.tmpl or template scaffold YAML file.")
	Cmd.Flags().StringVar(&flagSourceDir, flagNameSourceDir, "", "Policy source directory containing constraint.tmpl (gatekeeper-library layout).")
	Cmd.Flags().StringVar(&flagWorkingDir, flagNameWorkingDir, "", "Repository root used to resolve file.Read paths in constraint.tmpl. Inferred when omitted.")
	Cmd.Flags().StringArrayVar(&flagRegoPaths, flagNameRego, []string{}, "Rego file to inject when compiling a template scaffold without gomplate snippets. Can be specified multiple times.")
	Cmd.Flags().StringVar(&flagCelPath, flagNameCel, "", "CEL YAML file to inject into a K8sNativeValidation engine target.")
	Cmd.Flags().StringVarP(&flagOutput, flagNameOutput, "o", "", "Output file path. Prints to stdout when omitted.")
}

func run(_ *cobra.Command, _ []string) {
	if flagFilename == "" && flagSourceDir == "" {
		cmdutils.ErrFatalf("either --%s or --%s must be specified", flagNameFilename, flagNameSourceDir)
	}

	output, err := compile.Compile(compile.Options{
		TemplatePath: flagFilename,
		SourceDir:    flagSourceDir,
		WorkingDir:   flagWorkingDir,
		RegoPaths:    flagRegoPaths,
		CelPath:      flagCelPath,
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
