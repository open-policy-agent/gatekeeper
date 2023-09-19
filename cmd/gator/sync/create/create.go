package synccreate

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// createCmd represents the create command
var Cmd = &cobra.Command{
	Use:   "create",
	Short: "Create SyncSet(s) based on the requires-sync-data annotations in the input templates.",
	RunE:  runE,
}

var (
	flagFilenames        []string
	flagDiscoveryResults string
	flagImplementation   enumFlag
	flagOutput           enumFlag
)

const (
	flagNameTemplateFilename = "template-filename"
	flagNameDiscoveryResults = "discovery-results"
	flagNameImplementation   = "implementation"
	flagNameOutput           = "output"
)

func newEnumFlag(allowed []string, d string) *enumFlag {
	return &enumFlag{
		Allowed: allowed,
		Value:   d,
	}
}

type enumFlag struct {
	Allowed []string
	Value   string
}

// String is used both by fmt.Print and by Cobra in help text
func (o *enumFlag) String() string {
	return o.Value
}

// Set must have pointer receiver so it doesn't change the value of a copy
func (o *enumFlag) Set(v string) error {
	isIncluded := func(opts []string, val string) bool {
		for _, opt := range opts {
			if val == opt {
				return true
			}
		}
		return false
	}
	if !isIncluded(o.Allowed, v) {
		return fmt.Errorf("%s is not included in %s", v, strings.Join(o.Allowed, ","))
	}
	o.Value = v
	return nil
}

func (o *enumFlag) Type() string {
	return strings.Join(o.Allowed, "|")
}

func init() {
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameTemplateFilename, "f", []string{}, "a file or directory containing Constraint Templates.  Can be specified multiple times.")
	Cmd.MarkFlagRequired(flagNameTemplateFilename)

	Cmd.Flags().StringVarP(&flagDiscoveryResults, flagDiscoveryResults, "d", "", "a json string listing the GVKs supported by the cluster as a nested array of groups, containing supported versions, containing supported kinds.")
	flagImplementation := newEnumFlag([]string{"greedy", "optimal"}, "greedy")
	Cmd.Flags().VarP(flagImplementation, flagNameImplementation, "i", "the implementation to use for creating SyncSets.  One of: greedy|optimal.")
	flagOutput := newEnumFlag([]string{"single", "bundled"}, "bundled")
	Cmd.Flags().VarP(flagOutput, flagNameOutput, "o", "whether to bundle required GVKs into one SyncSet or output one SyncSet per template. One of: single|bundled.")
}

func runE(cmd *cobra.Command, args []string) error {
	return nil
}
