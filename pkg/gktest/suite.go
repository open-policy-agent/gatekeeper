package gktest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis"
	templatesv1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// scheme stores the k8s resource types we can instantiate as Templates.
var scheme = runtime.NewScheme()

func init() {
	_ = apis.AddToScheme(scheme)
}

// Suite defines a set of TestCases which all use the same ConstraintTemplate
// and Constraint.
type Suite struct {
	metav1.ObjectMeta

	// Template is the path to the Constraint Template, relative to the file
	// defining the Suite.
	Template string

	// Constraint is the path to the Constraint, relative to the file defining
	// the Suite.
	Constraint string

	TestCases []TestCase
}

var (
	// ErrNotATemplate indicates the user-indicated file does not contain a
	// ConstraintTemplate.
	ErrNotATemplate = errors.New("not a ConstraintTemplate")
	// ErrNotAConstraint indicates the user-indicated file does not contain a
	// Constraint.
	ErrNotAConstraint = errors.New("not a Constraint")
	// ErrAddingTemplate indicates a problem instantiating a Suite's ConstraintTemplate.
	ErrAddingTemplate = errors.New("adding template")
	// ErrAddingConstraint indicates a problem instantiating a Suite's Constraint.
	ErrAddingConstraint = errors.New("adding constraint")
	// ErrInvalidSuite indicates a Suite does not define the required fields.
	ErrInvalidSuite = errors.New("invalid Suite")
)

// readTemplate reads the contents of the path and returns the
// ConstraintTemplate it defines. Returns an error if the file does not define
// a ConstraintTemplate.
func readTemplate(f fs.FS, path string) (*templates.ConstraintTemplate, error) {
	bytes, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, fmt.Errorf("reading ConstraintTemplate from %q: %w", path, err)
	}

	u := unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}
	err = yaml.Unmarshal(bytes, u.Object)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing ConstraintTemplate YAML from %q: %v", ErrAddingTemplate, path, err)
	}

	gvk := u.GroupVersionKind()
	if gvk.Group != templatesv1.SchemeGroupVersion.Group || gvk.Kind != "ConstraintTemplate" {
		return nil, fmt.Errorf("%w: %q", ErrNotATemplate, path)
	}

	t, err := scheme.New(gvk)
	if err != nil {
		// The type isn't registered in the scheme.
		return nil, fmt.Errorf("%w: %v", ErrAddingTemplate, err)
	}

	// YAML parsing doesn't properly handle ObjectMeta, so we must
	// marshal/unmashal through JSON.
	jsonBytes, err := u.MarshalJSON()
	if err != nil {
		// Indicates a bug in unstructured.MarshalJSON(). Any Unstructured
		// unmarshalled from YAML should be marshallable to JSON.
		return nil, fmt.Errorf("calling unstructured.MarshalJSON(): %w", err)
	}
	err = json.Unmarshal(jsonBytes, t)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAddingTemplate, err)
	}

	template := &templates.ConstraintTemplate{}
	err = scheme.Convert(t, template, nil)
	if err != nil {
		// This shouldn't happen unless there's a bug in the conversion functions.
		// Most likely it means the conversion functions weren't generated.
		return nil, err
	}

	return template, nil
}

func readConstraint(f fs.FS, path string) (*unstructured.Unstructured, error) {
	bytes, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, fmt.Errorf("reading Constraint from %q: %w", path, err)
	}

	constraint := &unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}

	err = yaml.Unmarshal(bytes, constraint.Object)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing Constraint from %q: %v", ErrAddingConstraint, path, err)
	}

	gvk := constraint.GroupVersionKind()
	if gvk.Group != "constraints.gatekeeper.sh" {
		return nil, ErrNotAConstraint
	}

	return constraint, nil
}

// Run executes every TestCase in the Suite. Returns the results for every
// TestCase.
func (s *Suite) Run(ctx context.Context, c Client, f fs.FS, filter Filter) []Result {
	if s.Template == "" {
		return []Result{errorResult(fmt.Errorf("%w: missing template", ErrInvalidSuite))}
	}
	template, err := readTemplate(f, s.Template)
	if err != nil {
		return []Result{errorResult(err)}
	}
	_, err = c.AddTemplate(ctx, template)
	if err != nil {
		return []Result{errorResult(fmt.Errorf("%w: %v", ErrAddingTemplate, err))}
	}

	if s.Constraint == "" {
		return []Result{errorResult(fmt.Errorf("%w: missing constraint", ErrInvalidSuite))}
	}
	constraint, err := readConstraint(f, s.Constraint)
	if err != nil {
		return []Result{errorResult(err)}
	}
	_, err = c.AddConstraint(ctx, constraint)
	if err != nil {
		return []Result{errorResult(fmt.Errorf("%w: %v", ErrAddingConstraint, err))}
	}

	results := make([]Result, len(s.TestCases))
	for i, tc := range s.TestCases {
		if !filter.MatchesTest(tc) {
			continue
		}

		results[i] = tc.Run(f, c)
	}
	return results
}

// TestCase runs Constraint against a YAML object
type TestCase struct{}

// Run executes the TestCase and returns the Result of the run.
//
// Run never returns bare errors.  Use errorResult() to wrap errors in a Result
// if the test cannot be executed as intended.
func (tc TestCase) Run(f fs.FS, c Client) Result {
	return nil
}
