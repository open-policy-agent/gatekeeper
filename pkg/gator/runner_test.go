package gator

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/gatekeeper/pkg/gator/fixtures"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
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
					Error: ErrInvalidSuite,
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
					Error: ErrAddingTemplate,
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
					Error: ErrAddingTemplate,
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
					Error: ErrAddingTemplate,
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
					Error: ErrAddingTemplate,
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
					Error: ErrNotATemplate,
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
					Error: ErrInvalidSuite,
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
					Error: ErrValidConstraint,
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
					}},
				}, {
					Template:   "deny-template.yaml",
					Constraint: "deny-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: intStrFromStr("yes")}},
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
					}},
				}, {
					Template:   "deny-template.yaml",
					Constraint: "deny-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: intStrFromStr("yes")}},
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
					}},
				}, {
					Template:   "deny-template.yaml",
					Constraint: "deny-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: intStrFromStr("yes")}},
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
					}},
				}, {
					Template:   "deny-template.yaml",
					Constraint: "deny-constraint.yaml",
					Cases: []*Case{{
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: intStrFromStr("yes")}},
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
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
						Error: ErrInvalidYAML,
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
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
						Error: ErrNoObjects,
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
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
						Error: ErrMultipleObjects,
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
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
						Error: ErrNoObjects,
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
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
						Error: ErrAddInventory,
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
					Error: ErrAddingConstraint,
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
					Error: ErrNotAConstraint,
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
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
						Assertions: []Assertion{{Violations: intStrFromStr("yes")}},
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
					CaseResults: []CaseResult{{Error: ErrInvalidCase}},
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
					}, {
						Name:       "allowed-2",
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
					}},
				}, {
					Name:       "deny",
					Template:   "deny-template.yaml",
					Constraint: "deny-constraint.yaml",
					Cases: []*Case{{
						Name:       "denied",
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: intStrFromStr("yes")}},
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
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
					}, {
						Name:       "deny",
						Object:     "deny.yaml",
						Inventory:  []string{"inventory.yaml"},
						Assertions: []Assertion{{Violations: intStrFromStr("yes")}},
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
						Assertions: []Assertion{{Violations: intStrFromStr("yes")}},
					}, {
						Name:       "excluded",
						Object:     "excluded.yaml",
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
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
						Assertions: []Assertion{{Violations: intStrFromStr("yes")}},
					}, {
						Name:       "not-included",
						Object:     "not-included.yaml",
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
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
						Assertions: []Assertion{{Violations: intStrFromStr("yes")}},
					}, {
						Name:       "namespace-scope",
						Object:     "not-included.yaml",
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
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
						Assertions: []Assertion{{Violations: intStrFromStr("yes")}},
					}, {
						Name:       "not-selected",
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: intStrFromStr("no")}},
						Inventory:  []string{"inventory-2.yaml"},
					}, {
						Name:       "missing-namespace",
						Object:     "object.yaml",
						Assertions: []Assertion{{Violations: intStrFromStr("yes"), Message: pointer.StringPtr("missing Namespace")}},
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
	}

	for _, tc := range testCases {
		// Required for parallel tests.
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			runner, err := NewRunner(tc.f, NewOPAClient)
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
				t.Errorf(diff)
			}
		})
	}
}

func TestRunner_Run_ClientError(t *testing.T) {
	t.Parallel()

	want := SuiteResult{
		TestResults: []TestResult{{Error: ErrCreatingClient}},
	}

	runner, err := NewRunner(fstest.MapFS{}, func() (Client, error) {
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
			want:       CaseResult{Error: ErrInvalidCase},
		},
		{
			name:       "explicit expect allow boolean",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: intStrFromStr("no"),
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
				Error: ErrNumViolations,
			},
		},
		{
			name:       "explicit expect deny boolean fail",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{Violations: intStrFromStr("yes")}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "expect allow int",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: intStrFromInt(0),
			}},
			want: CaseResult{},
		},
		{
			name:       "expect deny int fail",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: intStrFromInt(1),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "expect deny message fail",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("first message"),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
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
			assertions: []Assertion{{Violations: intStrFromStr("yes")}},
			want:       CaseResult{},
		},
		{
			name:       "expect deny int",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: intStrFromInt(1),
			}},
			want: CaseResult{},
		},
		{
			name:       "expect deny int not enough violations",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: intStrFromInt(2),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "expect allow bool fail",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: intStrFromStr("no"),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "expect allow int fail",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: intStrFromInt(0),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "expect deny message",
			template:   fixtures.TemplateAlwaysValidate,
			constraint: fixtures.ConstraintAlwaysValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("never validate"),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		{
			name:       "message valid regex",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("[enrv]+ [adeiltv]+"),
			}},
			want: CaseResult{},
		},
		{
			name:       "message invalid regex",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("never validate [("),
			}},
			want: CaseResult{
				Error: ErrInvalidRegex,
			},
		},
		{
			name:       "message missing regex",
			template:   fixtures.TemplateNeverValidate,
			constraint: fixtures.ConstraintNeverValidate,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("[enrv]+x [adeiltv]+"),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
			},
		},
		// Deny multiple violations
		{
			name:       "multiple violations count",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: intStrFromInt(2),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation both messages implicit count",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("first message"),
			}, {
				Message: pointer.StringPtr("second message"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation both messages explicit count",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: intStrFromInt(1),
				Message:    pointer.StringPtr("first message"),
			}, {
				Violations: intStrFromInt(1),
				Message:    pointer.StringPtr("second message"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation regex implicit count",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("[cdefinorst]+ [aegms]+"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violation regex exact count",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Violations: intStrFromInt(2),
				Message:    pointer.StringPtr("[cdefinorst]+ [aegms]+"),
			}},
			want: CaseResult{},
		},
		{
			name:       "multiple violations and one missing message",
			template:   fixtures.TemplateNeverValidateTwice,
			constraint: fixtures.ConstraintNeverValidateTwice,
			object:     fixtures.Object,
			assertions: []Assertion{{
				Message: pointer.StringPtr("first message"),
			}, {
				Message: pointer.StringPtr("third message"),
			}},
			want: CaseResult{
				Error: ErrNumViolations,
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
				Error: ErrInvalidYAML,
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
				Error: ErrInvalidYAML,
			},
		},
	}

	const (
		templateFile   = "template.yaml"
		constraintFile = "constraint.yaml"
		objectFile     = "object.yaml"
	)

	for _, tc := range testCases {
		// Required for parallel tests.
		tc := tc

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
				NewOPAClient,
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
				t.Errorf(diff)
			}
		})
	}
}
