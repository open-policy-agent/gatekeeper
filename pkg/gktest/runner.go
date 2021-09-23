package gktest

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Runner defines logic independent of how tests are run and the results are
// printed.
type Runner struct {
	// FS is the filesystem the Runner interacts with to read Suites and objects.
	FS fs.FS

	// NewClient instantiates a Client for compiling Templates/Constraints, and
	// validating objects against them.
	NewClient func() (Client, error)
}

// Run executes all Tests in the Suite and returns the results.
func (r *Runner) Run(ctx context.Context, filter Filter, suitePath string, s *Suite) SuiteResult {
	start := time.Now()

	results, err := r.runTests(ctx, filter, suitePath, s.Tests)

	return SuiteResult{
		Path:        suitePath,
		Error:       err,
		Runtime:     Duration(time.Since(start)),
		TestResults: results,
	}
}

// runTests runs every Test in Suite.
func (r *Runner) runTests(ctx context.Context, filter Filter, suitePath string, tests []Test) ([]TestResult, error) {
	suiteDir := filepath.Dir(suitePath)

	results := make([]TestResult, len(tests))
	for i, t := range tests {
		if !filter.MatchesTest(t) {
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

	results, err := r.runCases(ctx, suiteDir, filter, t)

	return TestResult{
		Name:        t.Name,
		Error:       err,
		Runtime:     Duration(time.Since(start)),
		CaseResults: results,
	}
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

	_, err := newClient()
	if err != nil {
		return nil, err
	}

	results := make([]CaseResult, len(t.Cases))

	for i, c := range t.Cases {
		if !filter.MatchesCase(t.Name, c.Name) {
			results[i] = r.skipCase(c)
			continue
		}

		results[i] = r.runCase(ctx, newClient, suiteDir, c)
	}

	return results, nil
}

func (r *Runner) skipCase(c Case) CaseResult {
	return CaseResult{Name: c.Name, Skipped: true}
}

func (r *Runner) makeTestClient(ctx context.Context, suiteDir string, t Test) (Client, error) {
	client, err := r.NewClient()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCreatingClient, err)
	}

	err = r.addTemplate(ctx, suiteDir, t.Template, client)
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

	cObj, err := readConstraint(r.FS, filepath.Join(suiteDir, constraintPath))
	if err != nil {
		return err
	}

	_, err = client.AddConstraint(ctx, cObj)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAddingConstraint, err)
	}
	return nil
}

func (r *Runner) addTemplate(ctx context.Context, suiteDir, templatePath string, client Client) error {
	if templatePath == "" {
		return fmt.Errorf("%w: missing template", ErrInvalidSuite)
	}

	template, err := readTemplate(r.FS, filepath.Join(suiteDir, templatePath))
	if err != nil {
		return err
	}

	_, err = client.AddTemplate(ctx, template)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAddingTemplate, err)
	}

	return nil
}

// RunCase executes a Case and returns the result of the run.
func (r *Runner) runCase(ctx context.Context, newClient func() (Client, error), suiteDir string, c Case) CaseResult {
	start := time.Now()

	err := r.checkCase(ctx, newClient, suiteDir, c)

	return CaseResult{
		Name:    c.Name,
		Error:   err,
		Runtime: Duration(time.Since(start)),
	}
}

func (r *Runner) checkCase(ctx context.Context, newClient func() (Client, error), suiteDir string, c Case) (err error) {
	if c.Object == "" {
		return fmt.Errorf("%w: must define object", ErrInvalidCase)
	}

	objectPath := filepath.Join(suiteDir, c.Object)
	review, err := r.runReview(ctx, newClient, objectPath)
	if err != nil {
		return err
	}

	results := review.Results()

	if len(c.Assertions) == 0 {
		// Default to assuming the object passes validation if no Assertions are
		// defined.
		c.Assertions = []Assertion{{Violations: intStrFromStr("no")}}
	}

	for i := range c.Assertions {
		err = c.Assertions[i].Run(results)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Runner) runReview(ctx context.Context, newClient func() (Client, error), path string) (*types.Responses, error) {
	c, err := newClient()
	if err != nil {
		return nil, err
	}

	toReview, inventory, err := readCaseObjects(r.FS, path)
	if err != nil {
		return nil, err
	}

	for _, obj := range inventory {
		_, err = c.AddData(ctx, obj)
		if err != nil {
			return nil, err
		}
	}

	return c.Review(ctx, toReview)
}

// readCaseObjects reads objects at path in filesystem f. Returns:
// 1) The object to review for the test case
// 2) The objects to add to data.inventory
// 3) Any errors encountered parsing the objects
//
// The final object in path is the object to review.
func readCaseObjects(f fs.FS, path string) (*unstructured.Unstructured, []*unstructured.Unstructured, error) {
	bytes, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, nil, err
	}

	objs, err := readUnstructureds(bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %q: %w", path, err)
	}

	nObjs := len(objs)

	if nObjs == 0 {
		return nil, nil, fmt.Errorf("%w: path %q defines no YAML objects", ErrNoObjects, path)
	}

	toReview := objs[nObjs-1]
	inventory := objs[:nObjs-1]

	return toReview, inventory, nil
}
