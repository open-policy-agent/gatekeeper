package verify

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/gatekeeper/v3/apis"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	mutationtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// Runner defines logic independent of how tests are run and the results are
// printed.
type Runner struct {
	// filesystem is the filesystem the Runner interacts with to read Suites and objects.
	filesystem fs.FS

	// newClient instantiates a Client for compiling Templates/Constraints, and
	// validating objects against them.
	newClient func(includeTrace bool, useK8sCEL bool) (gator.Client, error)

	scheme *runtime.Scheme

	includeTrace bool

	useK8sCEL bool
}

func NewRunner(filesystem fs.FS, newClient func(includeTrace bool, useK8sCEL bool) (gator.Client, error), opts ...RunnerOptions) (*Runner, error) {
	s := runtime.NewScheme()
	err := apis.AddToScheme(s)
	if err != nil {
		return nil, err
	}

	r := &Runner{
		filesystem: filesystem,
		newClient:  newClient,
		scheme:     s,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r, nil
}

type RunnerOptions func(*Runner)

func IncludeTrace(includeTrace bool) RunnerOptions {
	return func(r *Runner) {
		r.includeTrace = includeTrace
	}
}

func UseK8sCEL(useK8sCEL bool) RunnerOptions {
	return func(r *Runner) {
		r.useK8sCEL = useK8sCEL
	}
}

// Run executes all Tests in the Suite and returns the results.
func (r *Runner) Run(ctx context.Context, filter Filter, s *Suite) SuiteResult {
	start := time.Now()

	if s.Skip {
		return SuiteResult{
			Path:    s.InputPath,
			Skipped: true,
		}
	}

	results, err := r.runTests(ctx, filter, s.AbsolutePath, s.Tests)

	return SuiteResult{
		Path:        s.InputPath,
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
		t := t
		if t.Skip || !filter.MatchesTest(&t) {
			results[i] = r.skipTest(&t)
			continue
		}

		results[i] = r.runTest(ctx, suiteDir, filter, &t)
	}

	return results, nil
}

func (r *Runner) skipTest(t *Test) TestResult {
	return TestResult{Name: t.Name, Skipped: true}
}

// runTest runs an individual Test.
func (r *Runner) runTest(ctx context.Context, suiteDir string, filter Filter, t *Test) TestResult {
	start := time.Now()

	err := r.tryAddConstraint(ctx, suiteDir, t)
	var results []CaseResult
	if t.Invalid {
		if errors.Is(err, constraints.ErrSchema) {
			err = nil
		} else {
			err = fmt.Errorf("%w: got error %w but want %w", gator.ErrValidConstraint, err, constraints.ErrSchema)
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

func (r *Runner) tryAddConstraint(ctx context.Context, suiteDir string, t *Test) error {
	client, err := r.newClient(r.includeTrace, r.useK8sCEL)
	if err != nil {
		return fmt.Errorf("%w: %w", gator.ErrCreatingClient, err)
	}

	err = r.addTemplate(suiteDir, t.Template, client)
	if err != nil {
		return err
	}

	constraintPath := t.Constraint
	if constraintPath == "" {
		return fmt.Errorf("%w: missing constraint", gator.ErrInvalidSuite)
	}

	cObj, err := reader.ReadConstraint(r.filesystem, path.Join(suiteDir, constraintPath))
	if err != nil {
		return err
	}

	_, err = client.AddConstraint(ctx, cObj)
	return err
}

// runCases executes every Case in the Test. Returns the results for every Case,
// or an error if there was a problem executing the Test.
func (r *Runner) runCases(ctx context.Context, suiteDir string, filter Filter, t *Test) ([]CaseResult, error) {
	newClient := func() (gator.Client, error) {
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

func (r *Runner) makeTestClient(ctx context.Context, suiteDir string, t *Test) (gator.Client, error) {
	client, err := r.newClient(r.includeTrace, r.useK8sCEL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", gator.ErrCreatingClient, err)
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

func (r *Runner) addConstraint(ctx context.Context, suiteDir, constraintPath string, client gator.Client) error {
	if constraintPath == "" {
		return fmt.Errorf("%w: missing constraint", gator.ErrInvalidSuite)
	}

	cObj, err := reader.ReadConstraint(r.filesystem, path.Join(suiteDir, constraintPath))
	if err != nil {
		return err
	}

	_, err = client.AddConstraint(ctx, cObj)
	if err != nil {
		return fmt.Errorf("%w: %w", gator.ErrAddingConstraint, err)
	}
	return nil
}

func (r *Runner) addTemplate(suiteDir, templatePath string, client gator.Client) error {
	if templatePath == "" {
		return fmt.Errorf("%w: missing template", gator.ErrInvalidSuite)
	}

	template, err := reader.ReadTemplate(r.scheme, r.filesystem, path.Join(suiteDir, templatePath))
	if err != nil {
		return err
	}

	_, err = client.AddTemplate(context.Background(), template)
	if err != nil {
		return fmt.Errorf("%w: %w", gator.ErrAddingTemplate, err)
	}

	return nil
}

// RunCase executes a Case and returns the result of the run.
func (r *Runner) runCase(ctx context.Context, newClient func() (gator.Client, error), suiteDir string, tc *Case) CaseResult {
	start := time.Now()
	trace, err := r.checkCase(ctx, newClient, suiteDir, tc)

	return CaseResult{
		Name:    tc.Name,
		Error:   err,
		Runtime: Duration(time.Since(start)),
		Trace:   trace,
	}
}

func (r *Runner) checkCase(ctx context.Context, newClient func() (gator.Client, error), suiteDir string, tc *Case) (trace *string, err error) {
	if tc.Object == "" {
		return nil, fmt.Errorf("%w: must define object", gator.ErrInvalidCase)
	}

	if len(tc.Assertions) == 0 {
		// Test cases must define at least one assertion.
		return nil, fmt.Errorf("%w: assertions must be non-empty", gator.ErrInvalidCase)
	}

	review, err := r.runReview(ctx, newClient, suiteDir, tc)
	if err != nil {
		return nil, err
	}

	results := review.Results()
	if r.includeTrace {
		trace = ptr.To[string](review.TraceDump())
	}
	for i := range tc.Assertions {
		err = tc.Assertions[i].Run(results)
		if err != nil {
			return trace, err
		}
	}

	return trace, nil
}

func (r *Runner) runReview(ctx context.Context, newClient func() (gator.Client, error), suiteDir string, tc *Case) (*types.Responses, error) {
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
			gator.ErrMultipleObjects, toReviewPath, len(toReviewObjs))
	}
	toReview := toReviewObjs[0]

	for _, p := range tc.Inventory {
		err = r.addInventory(ctx, c, suiteDir, p)
		if err != nil {
			return nil, err
		}
	}

	// check to see if obj is an AdmissionReview kind
	if toReview.GetKind() == "AdmissionReview" && toReview.GroupVersionKind().Group == admissionv1.SchemeGroupVersion.Group {
		return r.validateAndReviewAdmissionReviewRequest(ctx, c, toReview)
	}

	// otherwise our object is some other k8s object
	au := target.AugmentedUnstructured{
		Object: *toReview,
		Source: mutationtypes.SourceTypeOriginal,
	}
	return c.Review(ctx, au, reviews.EnforcementPoint(util.GatorEnforcementPoint))
}

func (r *Runner) validateAndReviewAdmissionReviewRequest(ctx context.Context, c gator.Client, toReview *unstructured.Unstructured) (*types.Responses, error) {
	// convert unstructured into AdmissionReview, don't allow unknown fields
	var ar admissionv1.AdmissionReview
	if err := runtime.DefaultUnstructuredConverter.FromUnstructuredWithValidation(toReview.UnstructuredContent(), &ar, true); err != nil {
		return nil, fmt.Errorf("%w: unable to convert to an AdmissionReview object, error: %w", gator.ErrInvalidK8sAdmissionReview, err)
	}

	if ar.Request == nil { // then this admission review did not actually pass in an AdmissionRequest
		return nil, fmt.Errorf("%w: request did not actually pass in an AdmissionRequest", gator.ErrMissingK8sAdmissionRequest)
	}

	// validate the AdmissionReview to match k8s api server behavior
	if ar.Request.Object.Raw == nil && ar.Request.OldObject.Raw == nil {
		return nil, fmt.Errorf("%w: AdmissionRequest does not contain an \"object\" or \"oldObject\" to review", gator.ErrNoObjectForReview)
	}

	// make sure that kind is set for object, oldObject if present since we expect it on Decode
	// https://github.com/kubernetes/apimachinery/blob/27a96d86e70e9bbd40639a0539a14423f55afa07/pkg/apis/meta/v1/unstructured/helpers.go#L341
	obj := &unstructured.Unstructured{}
	if ar.Request.Object.Raw != nil {
		if err := obj.UnmarshalJSON(ar.Request.Object.Raw); err != nil {
			return nil, fmt.Errorf("%w: %w", gator.ErrUnmarshallObject, err)
		}
	}
	if ar.Request.OldObject.Raw != nil {
		if err := obj.UnmarshalJSON(ar.Request.OldObject.Raw); err != nil {
			return nil, fmt.Errorf("%w: %w", gator.ErrUnmarshallObject, err)
		}
	}

	req := &admission.Request{AdmissionRequest: *ar.Request}
	if err := util.SetObjectOnDelete(req); err != nil {
		return nil, fmt.Errorf("%w: %w", gator.ErrInvalidK8sAdmissionReview, err)
	}

	arr := target.AugmentedReview{
		AdmissionRequest: &req.AdmissionRequest,
		Source:           mutationtypes.SourceTypeOriginal,
	}

	return c.Review(ctx, arr, reviews.EnforcementPoint(util.GatorEnforcementPoint))
}

func (r *Runner) addInventory(ctx context.Context, c gator.Client, suiteDir, inventoryPath string) error {
	p := path.Join(suiteDir, inventoryPath)

	inventory, err := readObjects(r.filesystem, p)
	if err != nil {
		return err
	}

	for _, obj := range inventory {
		_, err = c.AddData(ctx, obj)
		if err != nil {
			return fmt.Errorf("%w: %v %v/%v: %w",
				gator.ErrAddInventory, obj.GroupVersionKind(), obj.GetNamespace(), obj.GetName(), err)
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

	objs, err := reader.ReadUnstructureds(bytes)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}

	nObjs := len(objs)
	if nObjs == 0 {
		return nil, fmt.Errorf("%w: path %q defines no YAML objects", gator.ErrNoObjects, path)
	}

	return objs, nil
}
