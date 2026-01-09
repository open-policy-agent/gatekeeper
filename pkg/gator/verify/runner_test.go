package verify

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	clienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/fixtures"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func TestRunner_Run(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		filter string
		suite  Suite
		f      fs.FS
		want   SuiteResult
	}{
		{
			name: "Suite missing Template",
			suite: Suite{
				Tests: []Test{{}},
			},
			f: fstest.MapFS{},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: gator.ErrInvalidSuite,
				}},
			},
		},
		{
			name: "Suite with template in nonexistent file",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: fs.ErrNotExist,
				}},
			},
		},
		{
			name: "Suite with YAML parsing error",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateInvalidYAML),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: gator.ErrAddingTemplate,
				}},
			},
		},
		{
			name: "Suite with template unmarshalling error",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateMarshalError),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: gator.ErrAddingTemplate,
				}},
			},
		},
		{
			name: "Suite with rego compilation error",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateCompileError),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: gator.ErrAddingTemplate,
				}},
			},
		},
		{
			name: "Suite with unsupported template version",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateUnsupportedVersion),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: gator.ErrAddingTemplate,
				}},
			},
		},
		{
			name: "Suite pointing to non-template",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: gator.ErrNotATemplate,
				}},
			},
		},
		{
			name: "Suite missing Constraint",
			suite: Suite{
				Tests: []Test{{
					Template: "template.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: gator.ErrInvalidSuite,
				}},
			},
		},
		{
			name: "want invalid Constraint",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Invalid:    true,
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateRequiredLabel),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintRequireLabelInvalid),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{}},
			},
		},
		{
			name: "want invalid Constraint but Constraint valid",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Invalid:    true,
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateRequiredLabel),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintRequireLabelValid),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: gator.ErrValidConstraint,
				}},
			},
		},
		{
			name: "valid Suite",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}, {
					Template:   "deny-template.yaml",
					Constraint: "deny-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
				"deny-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateNeverValidate),
				},
				"deny-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintNeverValidate),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(fixtures.Object),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{}},
				}, {
					CaseResults: []CaseResult{{}},
				}},
			},
		},
		{
			name: "skip Case",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Skip:       true,
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}, {
					Template:   "deny-template.yaml",
					Constraint: "deny-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
				"deny-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateNeverValidate),
				},
				"deny-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintNeverValidate),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(fixtures.Object),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{
						Skipped: true,
					}},
				}, {
					CaseResults: []CaseResult{{}},
				}},
			},
		},
		{
			name: "skip Test",
			suite: Suite{
				Tests: []Test{{
					Skip:       true,
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}, {
					Template:   "deny-template.yaml",
					Constraint: "deny-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
				"deny-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateNeverValidate),
				},
				"deny-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintNeverValidate),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(fixtures.Object),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Skipped: true,
				}, {
					CaseResults: []CaseResult{{}},
				}},
			},
		},
		{
			name: "skip Suite",
			suite: Suite{
				Skip: true,
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}, {
					Template:   "deny-template.yaml",
					Constraint: "deny-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
				"deny-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateNeverValidate),
				},
				"deny-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintNeverValidate),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(fixtures.Object),
				},
			},
			want: SuiteResult{
				Skipped: true,
			},
		},
		{
			name: "invalid object",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectInvalid),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{
						Error: gator.ErrInvalidYAML,
					}},
				}},
			},
		},
		{
			name: "empty inventory",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Inventory:  []string{"inventory.yaml"},
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(fixtures.Object),
				},
				"inventory.yaml": &fstest.MapFile{
					Data: []byte(""),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{
						Error: gator.ErrNoObjects,
					}},
				}},
			},
		},
		{
			name: "multiple objects",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectMultiple),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{
						Error: gator.ErrMultipleObjects,
					}},
				}},
			},
		},
		{
			name: "no object",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectEmpty),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{
						Error: gator.ErrNoObjects,
					}},
				}},
			},
		},
		{
			name: "invalid inventory",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Inventory:  []string{"inventory.yaml"},
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(fixtures.Object),
				},
				"inventory.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectInvalidInventory),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{
						Error: gator.ErrAddInventory,
					}},
				}},
			},
		},
		{
			name: "valid Suite no cases",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{}},
			},
		},
		{
			name: "constraint missing file",
			suite: Suite{
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: fs.ErrNotExist,
				}},
			},
		},
		{
			name: "constraint invalid YAML",
			suite: Suite{
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintInvalidYAML),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: gator.ErrAddingConstraint,
				}},
			},
		},
		{
			name: "constraint is not a constraint",
			suite: Suite{
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: gator.ErrNotAConstraint,
				}},
			},
		},
		{
			name: "constraint is for other template",
			suite: Suite{
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintWrongTemplate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Error: constraintclient.ErrMissingConstraintTemplate,
				}},
			},
		},
		{
			name: "allow case missing file",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{
						Error: fs.ErrNotExist,
					}},
				}},
			},
		},
		{
			name: "deny case missing file",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{
						Error: fs.ErrNotExist,
					}},
				}},
			},
		},
		{
			name: "case without Object",
			suite: Suite{
				Tests: []Test{{
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases:      []*Case{{}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{{Error: gator.ErrInvalidCase}},
				}},
			},
		},
		{
			name:   "valid Suite with filter",
			filter: "allowed-2",
			suite: Suite{
				Tests: []Test{{
					Name:       "allow",
					Template:   "allow-template.yaml",
					Constraint: "allow-constraint.yaml",
					Cases: []*Case{{
						Name:       "allowed-1",
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}, {
						Name:       "allowed-2",
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}, {
					Name:       "deny",
					Template:   "deny-template.yaml",
					Constraint: "deny-constraint.yaml",
					Cases: []*Case{{
						Name:       "denied",
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"allow-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateAlwaysValidate),
				},
				"allow-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidate),
				},
				"deny-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateNeverValidate),
				},
				"deny-constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintNeverValidate),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(fixtures.Object),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Name: "allow",
					CaseResults: []CaseResult{{
						Name: "allowed-1", Skipped: true,
					}, {
						Name: "allowed-2",
					}},
				}, {
					Name: "deny", Skipped: true,
				}},
			},
		},
		{
			name: "referential constraints",
			suite: Suite{
				Tests: []Test{{
					Name:       "referential constraint",
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
					Cases: []*Case{{
						Name:       "allow",
						Object:     "allow.yaml",
						Inventory:  []string{"inventory.yaml"},
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}, {
						Name:       "deny",
						Object:     "deny.yaml",
						Inventory:  []string{"inventory.yaml"},
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateReferential),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintReferential),
				},
				"allow.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectReferentialAllow),
				},
				"deny.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectReferentialDeny),
				},
				"inventory.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectReferentialInventory),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Name: "referential constraint",
					CaseResults: []CaseResult{{
						Name: "allow",
					}, {
						Name: "deny",
					}},
				}},
			},
		},
		{
			name: "excluded namespace",
			suite: Suite{
				Tests: []Test{{
					Name:       "excluded namespace Constraint",
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
					Cases: []*Case{{
						Name:       "included",
						Object:     "included.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
					}, {
						Name:       "excluded",
						Object:     "excluded.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateNeverValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintExcludedNamespace),
				},
				"included.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectIncluded),
				},
				"excluded.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectExcluded),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Name: "excluded namespace Constraint",
					CaseResults: []CaseResult{{
						Name: "included",
					}, {
						Name: "excluded",
					}},
				}},
			},
		},
		{
			name: "included namespace",
			suite: Suite{
				Tests: []Test{{
					Name:       "included namespace Constraint",
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
					Cases: []*Case{{
						Name:       "included",
						Object:     "included.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
					}, {
						Name:       "not-included",
						Object:     "not-included.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateNeverValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintIncludedNamespace),
				},
				"included.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectIncluded),
				},
				"not-included.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectExcluded),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Name: "included namespace Constraint",
					CaseResults: []CaseResult{{
						Name: "included",
					}, {
						Name: "not-included",
					}},
				}},
			},
		},
		{
			name: "cluster scope",
			suite: Suite{
				Tests: []Test{{
					Name:       "cluster scope Constraint",
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
					Cases: []*Case{{
						Name:       "cluster-scope",
						Object:     "included.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
					}, {
						Name:       "namespace-scope",
						Object:     "not-included.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateNeverValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintClusterScope),
				},
				"included.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectClusterScope),
				},
				"not-included.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectNamespaceScope),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Name: "cluster scope Constraint",
					CaseResults: []CaseResult{{
						Name: "cluster-scope",
					}, {
						Name: "namespace-scope",
					}},
				}},
			},
		},
		{
			name: "namespace selector",
			suite: Suite{
				Tests: []Test{{
					Name:       "namespace selected Constraint",
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
					Cases: []*Case{{
						Name:       "selected",
						Object:     "object.yaml",
						Inventory:  []string{"inventory.yaml"},
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
					}, {
						Name:       "not-selected",
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
						Inventory:  []string{"inventory-2.yaml"},
					}, {
						Name:       "missing-namespace",
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes"), Message: ptr.To[string]("missing Namespace")}},
					}},
				}},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateNeverValidate),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintNamespaceSelector),
				},
				"object.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectNamespaceScope),
				},
				"inventory.yaml": &fstest.MapFile{
					Data: []byte(fixtures.NamespaceSelected),
				},
				"inventory-2.yaml": &fstest.MapFile{
					Data: []byte(fixtures.NamespaceNotSelected),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{{
					Name: "namespace selected Constraint",
					CaseResults: []CaseResult{{
						Name: "selected",
					}, {
						Name: "not-selected",
					}, {
						Name: "missing-namespace",
					}},
				}},
			},
		},
		{
			name: "admission review object",
			suite: Suite{
				Tests: []Test{
					{
						Name:       "userInfo admission review object", // this test checks that the user name start with "system:"
						Template:   "template.yaml",
						Constraint: "constraint.yaml",
						Cases: []*Case{
							{
								Name:       "user begins with \"system:\"",
								Object:     "system-ar.yaml",
								Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
							},
							{
								Name:       "user doesn't begin with \"system:\"",
								Object:     "non-system-ar.yaml",
								Assertions: []Assertion{{Violations: gator.IntStrFromStr("yes"), Message: ptr.To[string]("username is not allowed to perform this operation")}},
							},
						},
					},
					{
						Name:       "AdmissionReview with oldObject", // this test makes sure that we are submitting an old object for review
						Template:   "template.yaml",
						Constraint: "constraint.yaml",
						Cases: []*Case{
							{
								Name:       "oldObject submits for review",
								Object:     "oldObject-ar.yaml",
								Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
							},
						},
					},
					{
						Name:       "invalid admission review usage", // this test covers error handling for invalid admission review objects
						Template:   "template.yaml",
						Constraint: "constraint.yaml",
						Cases: []*Case{
							{
								Name:       "invalid admission review object", // this is an AdmissionReview with unknown fields
								Object:     "invalid-ar.yaml",
								Assertions: []Assertion{{}},
							},
							{
								Name:       "missing admission request object", // this is a blank AdmissionReview test
								Object:     "missing-ar-ar.yaml",
								Assertions: []Assertion{{}},
							},
							{
								Name:       "no objects to review", // this is an AdmissionRequest with no objects to review
								Object:     "no-objects-ar.yaml",
								Assertions: []Assertion{{}},
							},
							{
								Name:       "no oldObject on delete", // this is an AdmissionRequest w a DELETE operation but no oldObject provided
								Object:     "no-oldObject-ar.yaml",
								Assertions: []Assertion{{}},
							},
						},
					},
					{
						Name:       "invalid admission review request usage", // this test covers error handling for invalid admission review objects
						Template:   "template.yaml",
						Constraint: "constraint-with-match.yaml",
						Cases: []*Case{
							{
								Name:       "no kind on object", // this is an AdmissionRequest w a DELETE operation but no oldObject provided
								Object:     "no-kind-object-ar.yaml",
								Assertions: []Assertion{{}},
							},
							{
								Name:       "no kind on old object", // this is an AdmissionRequest w a DELETE operation but no oldObject provided
								Object:     "no-kind-oldObject-ar.yaml",
								Assertions: []Assertion{{}},
							},
						},
					},
				},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateValidateUserInfo),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidateUserInfo),
				},
				"constraint-with-match.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintAlwaysValidateUserInfoWithMatch),
				},
				"no-objects-ar.yaml": &fstest.MapFile{
					Data: []byte(fixtures.AdmissionReviewMissingObjectAndOldObject),
				},
				"system-ar.yaml": &fstest.MapFile{
					Data: []byte(fixtures.SystemAdmissionReview),
				},
				"non-system-ar.yaml": &fstest.MapFile{
					Data: []byte(fixtures.NonSystemAdmissionReview),
				},
				"invalid-ar.yaml": &fstest.MapFile{
					Data: []byte(fixtures.InvalidAdmissionReview),
				},
				"missing-ar-ar.yaml": &fstest.MapFile{
					Data: []byte(fixtures.AdmissionReviewMissingRequest),
				},
				"oldObject-ar.yaml": &fstest.MapFile{
					Data: []byte(fixtures.AdmissionReviewWithOldObject),
				},
				"no-oldObject-ar.yaml": &fstest.MapFile{
					Data: []byte(fixtures.DeleteAdmissionReviewWithNoOldObject),
				},
				"no-kind-object-ar.yaml": &fstest.MapFile{
					Data: []byte(fixtures.SystemAdmissionReviewMissingKind),
				},
				"no-kind-oldObject-ar.yaml": &fstest.MapFile{
					Data: []byte(fixtures.DeleteAdmissionReviewWithOldObjectMissingKind),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{
					{
						Name: "userInfo admission review object",
						CaseResults: []CaseResult{
							{Name: "user begins with \"system:\""},
							{Name: "user doesn't begin with \"system:\""},
						},
					},
					{
						Name: "AdmissionReview with oldObject",
						CaseResults: []CaseResult{
							{Name: "oldObject submits for review"},
						},
					},
					{
						Name: "invalid admission review usage",
						CaseResults: []CaseResult{
							{Name: "invalid admission review object", Error: gator.ErrInvalidK8sAdmissionReview},
							{Name: "missing admission request object", Error: gator.ErrMissingK8sAdmissionRequest},
							{Name: "no objects to review", Error: gator.ErrNoObjectForReview},
							{Name: "no oldObject on delete", Error: &clienterrors.ErrorMap{target.Name: constraintclient.ErrReview}},
						},
					},
					{
						Name: "invalid admission review request usage",
						CaseResults: []CaseResult{
							{Name: "no kind on object", Error: gator.ErrUnmarshallObject},
							{Name: "no kind on old object", Error: gator.ErrUnmarshallObject},
						},
					},
				},
			},
		},
		{
			name: "expansion system",
			suite: Suite{
				Tests: []Test{
					{
						Name:       "check custom field with expansion system",
						Template:   "template.yaml",
						Constraint: "constraint.yaml",
						Expansion:  "foo-expansion.yaml",
						Cases: []*Case{
							{
								Name:       "Foo Template object",
								Object:     "foo-template.yaml",
								Assertions: []Assertion{{Message: ptr.To[string]("Foo object has restricted custom field")}},
							},
						},
					},
					{
						Name:       "check custom field without expansion system",
						Template:   "template.yaml",
						Constraint: "constraint.yaml",
						Cases: []*Case{
							{
								Name:       "Foo Template object",
								Object:     "foo-template.yaml",
								Assertions: []Assertion{{Violations: gator.IntStrFromStr("no")}},
							},
						},
					},
					{
						Name:       "check custom field with multiple expansions",
						Template:   "template.yaml",
						Constraint: "constraint.yaml",
						Expansion:  "foobar-expansion.yaml",
						Cases: []*Case{
							{
								Name:   "FooBar Template object",
								Object: "foobar-template.yaml",
								Assertions: []Assertion{
									{
										Message: ptr.To[string]("Foo object has restricted custom field"),
									},
									{
										Message: ptr.To[string]("Bar object has restricted custom field"),
									},
								},
							},
						},
					},
				},
			},
			f: fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.TemplateRestrictCustomField),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ConstraintRestrictCustomField),
				},
				"foo-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectFooTemplate),
				},
				"foobar-template.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ObjectFooBarTemplate),
				},
				"foo-expansion.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ExpansionRestrictCustomField),
				},
				"foobar-expansion.yaml": &fstest.MapFile{
					Data: []byte(fixtures.ExpansionsFooBarTemplateToFooAndBar),
				},
			},
			want: SuiteResult{
				TestResults: []TestResult{
					{
						Name: "check custom field with expansion system",
						CaseResults: []CaseResult{
							{Name: "Foo Template object"},
						},
					},
					{
						Name: "check custom field without expansion system",
						CaseResults: []CaseResult{
							{Name: "Foo Template object"},
						},
					},
					{
						Name: "check custom field with multiple expansions",
						CaseResults: []CaseResult{
							{Name: "FooBar Template object"},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			runner, err := NewRunner(tc.f, func() (gator.Client, error) {
				return gator.NewOPAClient(false)
			})
			if err != nil {
				t.Fatal(err)
			}

			filter, err := NewFilter(tc.filter)
			if err != nil {
				t.Fatal(err)
			}

			got := runner.Run(ctx, filter, &tc.suite)

			if diff := cmp.Diff(tc.want, got, cmpopts.EquateErrors(), cmpopts.EquateEmpty(),
				cmpopts.IgnoreFields(SuiteResult{}, "Runtime"), cmpopts.IgnoreFields(TestResult{}, "Runtime"), cmpopts.IgnoreFields(CaseResult{}, "Runtime"),
			); diff != "" {
				t.Errorf("%s", diff)
			}
		})
	}
}

func TestRunner_Run_ClientError(t *testing.T) {
	t.Parallel()

	want := SuiteResult{
		TestResults: []TestResult{{Error: gator.ErrCreatingClient}},
	}

	runner, err := NewRunner(fstest.MapFS{}, func() (gator.Client, error) {
		return nil, errors.New("error")
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	suite := &Suite{
		Tests: []Test{{}},
	}
	got := runner.Run(ctx, &nilFilter{}, suite)

	if diff := cmp.Diff(want, got, cmpopts.EquateErrors(), cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(SuiteResult{}, "Runtime"), cmpopts.IgnoreFields(TestResult{}, "Runtime"), cmpopts.IgnoreFields(CaseResult{}, "Runtime"),
	); diff != "" {
		t.Error(diff)
	}
}

func TestRunner_RunCase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		template   string
		constraint string
		object     string
		assertions []Assertion
		want       CaseResult
	}{
		// Validation successful
		{
			name:       "no assertions is error",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: nil,
			want:       CaseResult{Error: gator.ErrInvalidCase},
		},
		{
			name:       "explicit expect allow boolean",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: gator.IntStrFromStr("no"),
			}},
			want: CaseResult{},
		},
		{
			name:       "implicit expect deny fail",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{}},
			want: CaseResult{
				Error: gator.ErrNumViolations,
			},
		},
		{
			name:       "explicit expect deny boolean fail",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
			want: CaseResult{
				Error: gator.ErrNumViolations,
			},
		},
		{
			name:       "expect allow int",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: gator.IntStrFromInt(0),
			}},
			want: CaseResult{},
		},
		{
			name:       "expect deny int fail",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: gator.IntStrFromInt(1),
			}},
			want: CaseResult{
				Error: gator.ErrNumViolations,
			},
		},
		{
			name:       "expect deny message fail",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: ptr.To[string]("first message"),
			}},
			want: CaseResult{
				Error: gator.ErrNumViolations,
			},
		},
		// Single violation
		{
			name:       "implicit expect deny",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{}},
			want:       CaseResult{},
		},
		{
			name:       "expect deny bool",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{Violations: gator.IntStrFromStr("yes")}},
			want:       CaseResult{},
		},
		{
			name:       "expect deny int",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: gator.IntStrFromInt(1),
			}},
			want: CaseResult{},
		},
		{
			name:       "expect deny int not enough violations",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: gator.IntStrFromInt(2),
			}},
			want: CaseResult{
				Error: gator.ErrNumViolations,
			},
		},
		{
			name:       "expect allow bool fail",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: gator.IntStrFromStr("no"),
			}},
			want: CaseResult{
				Error: gator.ErrNumViolations,
			},
		},
		{
			name:       "expect allow int fail",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: gator.IntStrFromInt(0),
			}},
			want: CaseResult{
				Error: gator.ErrNumViolations,
			},
		},
		{
			name:       "expect deny message",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: ptr.To[string]("never validate"),
			}},
			want: CaseResult{
				Error: gator.ErrNumViolations,
			},
		},
		{
			name:       "message valid regex",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: ptr.To[string]("[enrv]+ [adeiltv]+"),
			}},
			want: CaseResult{},
		},
		{
			name:       "message invalid regex",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: ptr.To[string]("never validate [("),
			}},
			want: CaseResult{
				Error: gator.ErrInvalidRegex,
			},
		},
		{
			name:       "message missing regex",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: ptr.To[string]("[enrv]+x [adeiltv]+"),
			}},
			want: CaseResult{
				Error: gator.ErrNumViolations,
			},
		},
		// Deny multiple violations
		{
			name:       "multiple violations count",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: gator.IntStrFromInt(2),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation both messages implicit count",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: ptr.To[string]("first message"),
			}, {
				Message: ptr.To[string]("second message"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation both messages explicit count",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: gator.IntStrFromInt(1),
				Message:    ptr.To[string]("first message"),
			}, {
				Violations: gator.IntStrFromInt(1),
				Message:    ptr.To[string]("second message"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation regex implicit count",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: ptr.To[string]("[cdefinorst]+ [aegms]+"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation regex exact count",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: gator.IntStrFromInt(2),
				Message:    ptr.To[string]("[cdefinorst]+ [aegms]+"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violations and one missing message",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: ptr.To[string]("first message"),
			}, {
				Message: ptr.To[string]("third message"),
			}},
			want: CaseResult{
				Error: gator.ErrNumViolations,
			},
		},
		// Invalid assertions
		{
			name:       "invalid IntOrStr",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: &intstr.IntOrString{Type: 3},
			}},
			want: CaseResult{
				Error: gator.ErrInvalidYAML,
			},
		},
		{
			name:       "invalid IntOrStr string value",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: &intstr.IntOrString{Type: intstr.String, StrVal: "other"},
			}},
			want: CaseResult{
				Error: gator.ErrInvalidYAML,
			},
		},
	}

	const (
		templateFile   = "template.yaml"
		constraintFile = "constraint.yaml"
		objectFile     = "object.yaml"
	)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			suite := &Suite{
				Tests: []Test{{
					Template:   templateFile,
					Constraint: constraintFile,
					Cases: []*Case{{
						Object:     objectFile,
						Assertions: tc.assertions,
					}},
				}},
			}

			ctx := context.Background()

			runner, err := NewRunner(
				fstest.MapFS{
					templateFile:   &fstest.MapFile{Data: []byte(tc.template)},
					constraintFile: &fstest.MapFile{Data: []byte(tc.constraint)},
					objectFile:     &fstest.MapFile{Data: []byte(tc.object)},
				},
				func() (gator.Client, error) {
					return gator.NewOPAClient(false)
				},
			)
			if err != nil {
				t.Fatal(err)
			}

			got := runner.Run(ctx, &nilFilter{}, suite)

			want := SuiteResult{
				TestResults: []TestResult{{
					CaseResults: []CaseResult{tc.want},
				}},
			}

			if diff := cmp.Diff(want, got, cmpopts.EquateErrors(), cmpopts.EquateEmpty(),
				cmpopts.IgnoreFields(SuiteResult{}, "Runtime"), cmpopts.IgnoreFields(TestResult{}, "Runtime"), cmpopts.IgnoreFields(CaseResult{}, "Runtime"),
			); diff != "" {
				t.Errorf("%s", diff)
			}
		})
	}
}
