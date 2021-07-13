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

// Suite defines a set of Constraint tests.
type Suite struct {
	metav1.ObjectMeta

	// Tests is a list of Template&Constraint pairs, with tests to run on
	// each.
	Tests []Test
}

// Run executes all Tests in the Suite and returns the results.
func (s *Suite) Run(ctx context.Context, client Client, f fs.FS, filter Filter) SuiteResult {
	result := SuiteResult{
		TestResults: make([]TestResult, len(s.Tests)),
	}
	for i, c := range s.Tests {
		if filter.MatchesTest(c) {
			result.TestResults[i] = c.run(ctx, client, f, filter)
		}
	}
	return result
}

// Test defines a Template&Constraint pair to instantiate, and Cases to
// run on the instantiated Constraint.
type Test struct {
	// Template is the path to the ConstraintTemplate, relative to the file
	// defining the Suite.
	Template string

	// Constraint is the path to the Constraint, relative to the file defining
	// the Suite. Must be an instance of Template.
	Constraint string

	// Cases are the test cases to run on the instantiated Constraint.
	Cases []Case
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

	c := &unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}

	err = yaml.Unmarshal(bytes, c.Object)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing Constraint from %q: %v", ErrAddingConstraint, path, err)
	}

	gvk := c.GroupVersionKind()
	if gvk.Group != "constraints.gatekeeper.sh" {
		return nil, ErrNotAConstraint
	}

	return c, nil
}

// run executes every Case in the Test. Returns the results for every Case.
func (t Test) run(ctx context.Context, client Client, f fs.FS, filter Filter) TestResult {
	if t.Template == "" {
		return TestResult{Error: fmt.Errorf("%w: missing template", ErrInvalidSuite)}
	}
	template, err := readTemplate(f, t.Template)
	if err != nil {
		return TestResult{Error: err}
	}
	_, err = client.AddTemplate(ctx, template)
	if err != nil {
		return TestResult{Error: fmt.Errorf("%w: %v", ErrAddingTemplate, err)}
	}

	if t.Constraint == "" {
		return TestResult{Error: fmt.Errorf("%w: missing constraint", ErrInvalidSuite)}
	}
	cObj, err := readConstraint(f, t.Constraint)
	if err != nil {
		return TestResult{Error: err}
	}
	_, err = client.AddConstraint(ctx, cObj)
	if err != nil {
		return TestResult{Error: fmt.Errorf("%w: %v", ErrAddingConstraint, err)}
	}

	results := make([]CaseResult, len(t.Cases))
	for i, tc := range t.Cases {
		if !filter.MatchesCase(tc) {
			continue
		}

		results[i] = tc.run(f, client)
	}
	return TestResult{CaseResults: results}
}

// Case runs Constraint against a YAML object.
type Case struct{}

// run executes the Case and returns the Result of the run.
func (c Case) run(f fs.FS, client Client) CaseResult {
	return CaseResult{}
}
