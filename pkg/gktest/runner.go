package gktest

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
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

	result := make([]TestResult, len(tests))
	for i, t := range tests {
		if filter.MatchesTest(t) {
			result[i] = r.runTest(ctx, suiteDir, filter, t)
		}
	}

	return result, nil
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
	client, err := r.makeTestClient(ctx, suiteDir, t)
	if err != nil {
		return nil, err
	}

	results := make([]CaseResult, len(t.Cases))
	for i, c := range t.Cases {
		if !filter.MatchesCase(c) {
			continue
		}

		results[i] = r.runCase(ctx, client, suiteDir, c)
	}

	return results, nil
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
func (r *Runner) runCase(ctx context.Context, client Client, suiteDir string, c Case) CaseResult {
	start := time.Now()

	err := r.checkCase(ctx, client, suiteDir, c)

	return CaseResult{
		Name:    c.Name,
		Error:   err,
		Runtime: Duration(time.Since(start)),
	}
}

func (r *Runner) checkCase(ctx context.Context, client Client, suiteDir string, c Case) error {
	if c.Object == "" {
		return fmt.Errorf("%w: must define object", ErrInvalidCase)
	}

	objectPath := filepath.Join(suiteDir, c.Object)
	review, err := r.runReview(ctx, client, objectPath)
	if err != nil {
		return err
	}

	if c.Violations == nil {
		return checkAllow(review)
	}

	return checkDeny(review, *c.Violations)
}

func (r *Runner) runReview(ctx context.Context, client Client, path string) (*types.Responses, error) {
	u, err := readCase(r.FS, path)
	if err != nil {
		return nil, err
	}

	return client.Review(ctx, u)
}

func readCase(f fs.FS, path string) (*unstructured.Unstructured, error) {
	bytes, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, err
	}

	return readUnstructured(bytes)
}

func checkAllow(result *types.Responses) error {
	results := result.Results()
	if len(results) > 0 {
		return fmt.Errorf("%w: %v", ErrUnexpectedDeny, results[0].Msg)
	}
	return nil
}

func checkDeny(result *types.Responses, violations Violations) error {
	results := result.Results()
	if len(results) == 0 {
		return ErrUnexpectedAllow
	}

	err := checkCounts(results, violations)
	if err != nil {
		return err
	}

	return checkMessages(results, violations)
}

func checkCounts(results []*types.Result, violations Violations) error {
	gotViolations := int32(len(results))
	if violations.Count != nil && gotViolations != *violations.Count {
		return fmt.Errorf("%w: got %d violations but want %d", ErrNumViolations, len(results), *violations.Count)
	}

	return nil
}

func checkMessages(results []*types.Result, violations Violations) error {
	wantRegexes, err := compileRegexes(violations.Messages)
	if err != nil {
		return err
	}

	gotMessages := toGotMessages(results)

	for _, r := range wantRegexes {
		err = checkMessage(r, gotMessages)
		if err != nil {
			return err
		}
	}

	return nil
}

func checkMessage(r *regexp.Regexp, gotMessages []string) error {
	for _, got := range gotMessages {
		if r.MatchString(got) {
			return nil
		}
	}

	return fmt.Errorf("%w: got messages %s, missing message including %q", ErrMissingMessage, gotMessages, r.String())
}

func compileRegexes(regexes []string) ([]*regexp.Regexp, error) {
	rs := make([]*regexp.Regexp, len(regexes))
	for i, r := range regexes {
		var err error
		rs[i], err = regexp.Compile(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidRegex, err)
		}
	}

	return rs, nil
}

func toGotMessages(results []*types.Result) []string {
	gotMessages := make([]string, len(results))
	for i, r := range results {
		gotMessages[i] = r.Msg
	}

	return gotMessages
}
