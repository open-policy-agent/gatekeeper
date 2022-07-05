package gator

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/apis"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// Runner defines logic independent of how tests are run and the results are
// printed.
type Runner struct {
	// filesystem is the filesystem the Runner interacts with to read Suites and objects.
	filesystem fs.FS

	// newClient instantiates a Client for compiling Templates/Constraints, and
	// validating objects against them.
	newClient func() (Client, error)

	scheme *runtime.Scheme
}

func NewRunner(filesystem fs.FS, newClient func() (Client, error)) (*Runner, error) {
	s := runtime.NewScheme()
	err := apis.AddToScheme(s)
	if err != nil {
		return nil, err
	}

	return &Runner{
		filesystem: filesystem,
		newClient:  newClient,
		scheme:     s,
	}, nil
}

// Run executes all Tests in the Suite and returns the results.
func (r *Runner) Run(ctx context.Context, filter Filter, s *Suite) SuiteResult {
	start := time.Now()

	if s.Skip {
		return SuiteResult{
			Path:    s.Path,
			Skipped: true,
		}
	}

	results, err := r.runTests(ctx, filter, s.Path, s.Tests)

	return SuiteResult{
		Path:        s.Path,
		Error:       err,
		Runtime:     Duration(time.Since(start)),
		TestResults: results,
	}
}

// runTests runs every Test in Suite.
func (r *Runner) runTests(ctx context.Context, filter Filter, suitePath string, tests []Test) ([]TestResult, error) {
	suiteDir := path.Dir(suitePath)

	results := make([]TestResult, len(tests))
	for i, t := range tests {
		if t.Skip || !filter.MatchesTest(t) {
			results[i] = r.skipTest(t)
			continue
		}

		results[i] = r.runTest(ctx, suiteDir, filter, t)
	}

	return results, nil
}

func (r *Runner) skipTest(t Test) TestResult {
	return TestResult{Name: t.Name, Skipped: true}
}

// runTest runs an individual Test.
func (r *Runner) runTest(ctx context.Context, suiteDir string, filter Filter, t Test) TestResult {
	start := time.Now()

	err := r.tryAddConstraint(ctx, suiteDir, t)
	var results []CaseResult
	if t.Invalid {
		if errors.Is(err, constraints.ErrSchema) {
			err = nil
		} else {
			err = fmt.Errorf("%w: got error %v but want %v", ErrValidConstraint, err, constraints.ErrSchema)
		}
	} else if err == nil {
		results, err = r.runCases(ctx, suiteDir, filter, t)
	}

	return TestResult{
		Name:        t.Name,
		Error:       err,
		Runtime:     Duration(time.Since(start)),
		CaseResults: results,
	}
}

func (r *Runner) tryAddConstraint(ctx context.Context, suiteDir string, t Test) error {
	client, err := r.newClient()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCreatingClient, err)
	}

	err = r.addTemplate(suiteDir, t.Template, client)
	if err != nil {
		return err
	}

	constraintPath := t.Constraint
	if constraintPath == "" {
		return fmt.Errorf("%w: missing constraint", ErrInvalidSuite)
	}

	cObj, err := readConstraint(r.filesystem, path.Join(suiteDir, constraintPath))
	if err != nil {
		return err
	}

	_, err = client.AddConstraint(ctx, cObj)
	return err
}

// runCases executes every Case in the Test. Returns the results for every Case,
// or an error if there was a problem executing the Test.
func (r *Runner) runCases(ctx context.Context, suiteDir string, filter Filter, t Test) ([]CaseResult, error) {
	newClient := func() (Client, error) {
		c, err := r.makeTestClient(ctx, suiteDir, t)
		if err != nil {
			return nil, err
		}

		return c, nil
	}

	results := make([]CaseResult, len(t.Cases))

	for i, c := range t.Cases {
		if c.Skip || !filter.MatchesCase(t.Name, c.Name) {
			results[i] = r.skipCase(c)
			continue
		}

		results[i] = r.runCase(ctx, newClient, suiteDir, c)
	}

	return results, nil
}

func (r *Runner) skipCase(tc *Case) CaseResult {
	return CaseResult{Name: tc.Name, Skipped: true}
}

func (r *Runner) makeTestClient(ctx context.Context, suiteDir string, t Test) (Client, error) {
	client, err := r.newClient()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCreatingClient, err)
	}

	err = r.addTemplate(suiteDir, t.Template, client)
	if err != nil {
		return nil, err
	}

	err = r.addConstraint(ctx, suiteDir, t.Constraint, client)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (r *Runner) addConstraint(ctx context.Context, suiteDir, constraintPath string, client Client) error {
	if constraintPath == "" {
		return fmt.Errorf("%w: missing constraint", ErrInvalidSuite)
	}

	cObj, err := readConstraint(r.filesystem, path.Join(suiteDir, constraintPath))
	if err != nil {
		return err
	}

	_, err = client.AddConstraint(ctx, cObj)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAddingConstraint, err)
	}
	return nil
}

func (r *Runner) addTemplate(suiteDir, templatePath string, client Client) error {
	if templatePath == "" {
		return fmt.Errorf("%w: missing template", ErrInvalidSuite)
	}

	template, err := ReadTemplate(r.scheme, r.filesystem, path.Join(suiteDir, templatePath))
	if err != nil {
		return err
	}

	_, err = client.AddTemplate(context.Background(), template)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAddingTemplate, err)
	}

	return nil
}

// RunCase executes a Case and returns the result of the run.
func (r *Runner) runCase(ctx context.Context, newClient func() (Client, error), suiteDir string, tc *Case) CaseResult {
	start := time.Now()

	err := r.checkCase(ctx, newClient, suiteDir, tc)

	return CaseResult{
		Name:    tc.Name,
		Error:   err,
		Runtime: Duration(time.Since(start)),
	}
}

func (r *Runner) checkCase(ctx context.Context, newClient func() (Client, error), suiteDir string, tc *Case) (err error) {
	if tc.Object == "" {
		return fmt.Errorf("%w: must define object", ErrInvalidCase)
	}

	if len(tc.Assertions) == 0 {
		// Test cases must define at least one assertion.
		return fmt.Errorf("%w: assertions must be non-empty", ErrInvalidCase)
	}

	review, err := r.runReview(ctx, newClient, suiteDir, tc)
	if err != nil {
		return err
	}

	results := review.Results()
	for i := range tc.Assertions {
		err = tc.Assertions[i].Run(results)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Runner) runReview(ctx context.Context, newClient func() (Client, error), suiteDir string, tc *Case) (*types.Responses, error) {
	c, err := newClient()
	if err != nil {
		return nil, err
	}

	toReviewPath := path.Join(suiteDir, tc.Object)
	toReviewObjs, err := readObjects(r.filesystem, toReviewPath)
	if err != nil {
		return nil, err
	}
	if len(toReviewObjs) != 1 {
		return nil, fmt.Errorf("%w: %q defines %d objects",
			ErrMultipleObjects, toReviewPath, len(toReviewObjs))
	}
	toReview := toReviewObjs[0]

	for _, p := range tc.Inventory {
		err = r.addInventory(ctx, c, suiteDir, p)
		if err != nil {
			return nil, err
		}
	}

	return c.Review(ctx, toReview)
}

func (r *Runner) addInventory(ctx context.Context, c Client, suiteDir, inventoryPath string) error {
	p := path.Join(suiteDir, inventoryPath)

	inventory, err := readObjects(r.filesystem, p)
	if err != nil {
		return err
	}

	for _, obj := range inventory {
		_, err = c.AddData(ctx, obj)
		if err != nil {
			return fmt.Errorf("%w: %v %v/%v: %v",
				ErrAddInventory, obj.GroupVersionKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	}

	return nil
}

// readCaseObjects reads objects at path in filesystem f. Returns:
// 1) The object to review for the test case
// 2) The objects to add to data.inventory
// 3) Any errors encountered parsing the objects
//
// The final object in path is the object to review.
func readObjects(f fs.FS, path string) ([]*unstructured.Unstructured, error) {
	bytes, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, err
	}

	objs, err := readUnstructureds(bytes)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}

	nObjs := len(objs)
	if nObjs == 0 {
		return nil, fmt.Errorf("%w: path %q defines no YAML objects", ErrNoObjects, path)
	}

	return objs, nil
}
