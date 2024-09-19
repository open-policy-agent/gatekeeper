/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package readiness

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	externaldatav1beta1 "github.com/open-policy-agent/frameworks/constraint/pkg/apis/externaldata/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/gatekeeper/v3/apis"
	configv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/config/v1alpha1"
	expansionv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/expansion/v1alpha1"
	mutationv1 "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/v1"
	mutationsv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/mutations/v1alpha1"
	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/syncutil"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	timeout = 10 * time.Second
	tick    = 1 * time.Second
)

var (
	testConstraintTemplate = templates.ConstraintTemplate{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-contraint-template",
		},
		Spec: templates.ConstraintTemplateSpec{
			CRD: templates.CRD{
				Spec: templates.CRDSpec{
					Names: templates.Names{
						Kind: "test-constraint",
					},
				},
			},
		},
	}

	testSyncSet = syncsetv1alpha1.SyncSet{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-sycnset",
		},
		Spec: syncsetv1alpha1.SyncSetSpec{
			GVKs: []syncsetv1alpha1.GVKEntry{
				{Group: "", Version: "v1", Kind: "Pod"},
			},
		},
	}

	testConfig = configv1alpha1.Config{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-config",
		},
		Spec: configv1alpha1.ConfigSpec{},
	}

	testAssignMetadata = mutationsv1alpha1.AssignMetadata{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-assign-metadata",
		},
		Spec: mutationsv1alpha1.AssignMetadataSpec{
			Location: "",
		},
	}

	testAssign = mutationsv1alpha1.Assign{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-assign",
		},
		Spec: mutationsv1alpha1.AssignSpec{
			Location: "",
		},
	}

	testModifySet = mutationsv1alpha1.ModifySet{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-modify-set",
		},
		Spec: mutationsv1alpha1.ModifySetSpec{
			Location: "",
		},
	}

	testAssignImage = mutationsv1alpha1.AssignImage{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-assign-image",
		},
		Spec: mutationsv1alpha1.AssignImageSpec{
			Location: "",
		},
	}

	testExternalDataProvider = externaldatav1beta1.Provider{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-external-data-provider",
		},
		Spec: externaldatav1beta1.ProviderSpec{
			URL: "",
		},
	}

	testExpansionTemplate = expansionv1alpha1.ExpansionTemplate{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-expansion-templates",
		},
		Spec: expansionv1alpha1.ExpansionTemplateSpec{
			TemplateSource: "",
		},
	}

	podGVK = schema.GroupVersionKind{Version: "v1", Kind: "Pod"}
)

var convertedTemplate v1beta1.ConstraintTemplate

func init() {
	if err := fakes.GetTestScheme().Convert(&testConstraintTemplate, &convertedTemplate, nil); err != nil {
		panic(err)
	}
}

func getTestConstraint() *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetName("test-constraint")
	gvk := schema.GroupVersionKind{
		Group:   "constraints.gatekeeper.sh",
		Version: "v1beta1",
		Kind:    "FooKind",
	}
	u.SetGroupVersionKind(gvk)
	return u
}

func mustInitializeScheme(scheme *runtime.Scheme) *runtime.Scheme {
	if err := apis.AddToScheme(scheme); err != nil {
		panic(err)
	}

	return scheme
}

// Verify that TryCancelTemplate functions the same as regular CancelTemplate if readinessRetries is set to 0.
func Test_ReadyTracker_TryCancelTemplate_No_Retries(t *testing.T) {
	lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(convertedTemplate.DeepCopyObject()).Build()
	wrapLister := WrapFakeClientWithMutex{fakeLister: lister}

	rt := newTracker(&wrapLister, false, false, false, false, nil, func() objData {
		return objData{retries: 0}
	})

	// Run kicks off all the tracking
	ctx, cancel := context.WithCancel(context.Background())
	var runErr error
	runWg := sync.WaitGroup{}
	runWg.Add(1)
	go func() {
		runErr = rt.Run(ctx)
		runWg.Done()
	}()

	t.Cleanup(func() {
		cancel()
		runWg.Wait()
		if runErr != nil {
			t.Errorf("got Tracker.Run() error: %v, want %v", runErr, nil)
		}
	})

	require.Eventually(t, func() bool {
		return rt.Populated()
	}, timeout, tick, "waiting for RT to populated")

	if rt.Satisfied() {
		t.Fatal("tracker with 0 retries should not be satisfied")
	}

	rt.TryCancelTemplate(&testConstraintTemplate) // 0 retries --> DELETE

	if !rt.Satisfied() {
		t.Fatal("tracker with 0 retries and cancellation should be satisfied")
	}
}

// Verify that TryCancelTemplate must be called enough times to remove all retries before canceling a template.
func Test_ReadyTracker_TryCancelTemplate_Retries(t *testing.T) {
	lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(convertedTemplate.DeepCopyObject()).Build()
	wrapLister := WrapFakeClientWithMutex{fakeLister: lister}

	rt := newTracker(&wrapLister, false, false, false, false, nil, func() objData {
		return objData{retries: 2}
	})

	// Run kicks off all the tracking
	ctx, cancel := context.WithCancel(context.Background())
	var runErr error
	runWg := sync.WaitGroup{}
	runWg.Add(1)
	go func() {
		runErr = rt.Run(ctx)
		runWg.Done()
	}()

	t.Cleanup(func() {
		cancel()
		runWg.Wait()
		if runErr != nil {
			t.Errorf("Tracker Run() failed with error: %v", runErr)
		}
	})

	require.Eventually(t, func() bool {
		return rt.Populated()
	}, timeout, tick, "waiting for RT to populated")

	if rt.Satisfied() {
		t.Fatal("tracker with 2 retries should not be satisfied")
	}

	rt.TryCancelTemplate(&testConstraintTemplate) // 2 --> 1 retries

	if rt.Satisfied() {
		t.Fatal("tracker with 1 retries should not be satisfied")
	}

	rt.TryCancelTemplate(&testConstraintTemplate) // 1 --> 0 retries

	if rt.Satisfied() {
		t.Fatal("tracker with 0 retries should not be satisfied")
	}

	rt.TryCancelTemplate(&testConstraintTemplate) // 0 retries --> DELETE

	if !rt.Satisfied() {
		t.Fatal("tracker with 0 retries and cancellation should be satisfied")
	}
}

func Test_Tracker_TryCancelData(t *testing.T) {
	lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(
		&testSyncSet, fakes.UnstructuredFor(podGVK, "", "pod1-name"),
	).Build()
	tcs := []struct {
		name    string
		retries int
	}{
		{name: "no retries", retries: 0},
		{name: "with retries", retries: 2},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			objDataFn := func() objData {
				return objData{retries: tc.retries}
			}
			rt := newTracker(lister, false, false, false, false, nil, objDataFn)

			ctx, cancel := context.WithCancel(context.Background())
			var runErr error
			runWg := sync.WaitGroup{}
			runWg.Add(1)
			go func() {
				runErr = rt.Run(ctx)
				// wait for the ready tracker to stop so we don't leak state between tests.
				runWg.Done()
			}()

			require.Eventually(t, func() bool {
				return rt.Populated()
			}, timeout, tick, "waiting for RT to populated")
			require.False(t, rt.Satisfied(), "tracker with retries should not be satisfied")

			// observe the sync source for readiness
			rt.syncsets.Observe(&testSyncSet)

			for i := tc.retries; i > 0; i-- {
				require.False(t, rt.data.Satisfied(), "data tracker should not be satisfied")
				require.False(t, rt.Satisfied(), fmt.Sprintf("tracker with %d retries should not be satisfied", i))
				rt.TryCancelData(podGVK)
			}
			require.False(t, rt.Satisfied(), "tracker should not be satisfied")

			rt.TryCancelData(podGVK) // at this point there should no retries
			require.True(t, rt.Satisfied(), "tracker with 0 retries and cancellation should be satisfied")
			require.True(t, rt.data.Satisfied(), "data tracker should be satisfied")

			_, removed := rt.data.removed[podGVK]
			require.True(t, removed, "expected the podGVK to have been removed")

			// cleanup test
			cancel()
			runWg.Wait()
			require.NoError(t, runErr, "Tracker Run() failed")
		})
	}
}

func Test_ReadyTracker_TrackAssignMetadata(t *testing.T) {
	tcs := []struct {
		name            string
		expectedErrMsgs []string
		failClose       bool
	}{
		{
			name:            "TrackAssignMetadata fail close",
			expectedErrMsgs: []string{"listing AssignMetadata"},
			failClose:       true,
		},
		{
			name:            "TrackAssignMetadata fail open",
			expectedErrMsgs: []string{"listing AssignMetadata"},
			failClose:       false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*mutationv1.AssignMetadataList); ok {
					return fmt.Errorf("Force Test AssignMetadataList Failure")
				}
				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(&testAssignMetadata).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, true, false, false, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})
			errChan := make(chan error)
			errGatheringDone := make(chan struct{})

			errs := syncutil.NewConcurrentErrorSlice()
			go func() {
				defer close(errGatheringDone)
				for {
					err, ok := <-errChan
					if !ok {
						return
					}
					errs = errs.Append(err)
				}
			}()

			ctx := context.Background()
			rt.trackAssignMetadata(ctx, errChan)
			close(errChan)
			<-errGatheringDone

			gotErrs := errs.GetSlice()
			if len(gotErrs) != 1 {
				t.Errorf("unexpected number of errors returned: %+v, expected: 1, got: %v ", gotErrs, len(gotErrs))
			}

			if !strings.Contains(gotErrs[0].Error(), tc.expectedErrMsgs[0]) {
				t.Errorf("expected error to contain %v, but got error: %v ", tc.expectedErrMsgs[0], gotErrs[0])
			}

			expectPopulated := !tc.failClose
			if rt.assignMetadata.Populated() != expectPopulated {
				t.Fatalf("assignMetadata object tracker's populated field is marked as %v but should be %v", rt.assignMetadata.Populated(), expectPopulated)
			}
		})
	}
}

func Test_ReadyTracker_TrackAssign(t *testing.T) {
	tcs := []struct {
		name            string
		expectedErrMsgs []string
		failClose       bool
	}{
		{
			name:            "TrackAssign fail close",
			expectedErrMsgs: []string{"listing Assign"},
			failClose:       true,
		},
		{
			name:            "TrackAssign fail open",
			expectedErrMsgs: []string{"listing Assign"},
			failClose:       false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*mutationv1.AssignList); ok {
					return fmt.Errorf("Force Test AssignList Failure")
				}
				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(&testAssign).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, true, false, false, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})
			errChan := make(chan error)
			errGatheringDone := make(chan struct{})

			errs := syncutil.NewConcurrentErrorSlice()
			go func() {
				defer close(errGatheringDone)
				for {
					err, ok := <-errChan
					if !ok {
						return
					}
					errs = errs.Append(err)
				}
			}()

			ctx := context.Background()
			rt.trackAssign(ctx, errChan)
			close(errChan)
			<-errGatheringDone

			gotErrs := errs.GetSlice()
			if len(gotErrs) != 1 {
				t.Errorf("unexpected number of errors returned: %+v, expected: 1, got: %v ", gotErrs, len(gotErrs))
			}

			if !strings.Contains(gotErrs[0].Error(), tc.expectedErrMsgs[0]) {
				t.Errorf("expected error to contain %v, but got error: %v ", tc.expectedErrMsgs[0], gotErrs[0])
			}

			expectPopulated := !tc.failClose
			if rt.assign.Populated() != expectPopulated {
				t.Fatalf("assign object tracker's populated field is marked as %v but should be %v", rt.assign.Populated(), expectPopulated)
			}
		})
	}
}

func Test_ReadyTracker_TrackModifySet(t *testing.T) {
	tcs := []struct {
		name            string
		expectedErrMsgs []string
		failClose       bool
	}{
		{
			name:            "TrackModifySet fail close",
			expectedErrMsgs: []string{"listing ModifySet"},
			failClose:       true,
		},
		{
			name:            "TrackModifySet fail open",
			expectedErrMsgs: []string{"listing ModifySet"},
			failClose:       false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*mutationv1.ModifySetList); ok {
					return fmt.Errorf("Force Test ModifySetList Failure")
				}
				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(&testModifySet).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, true, false, false, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})
			errChan := make(chan error)
			errGatheringDone := make(chan struct{})

			errs := syncutil.NewConcurrentErrorSlice()
			go func() {
				defer close(errGatheringDone)
				for {
					err, ok := <-errChan
					if !ok {
						return
					}
					errs = errs.Append(err)
				}
			}()

			// Run test
			ctx := context.Background()
			rt.trackModifySet(ctx, errChan)
			close(errChan)
			<-errGatheringDone

			gotErrs := errs.GetSlice()
			if len(gotErrs) != 1 {
				t.Errorf("unexpected number of errors returned: %+v, expected: 1, got: %v ", gotErrs, len(gotErrs))
			}

			if !strings.Contains(gotErrs[0].Error(), tc.expectedErrMsgs[0]) {
				t.Errorf("expected error to contain %v, but got error: %v ", tc.expectedErrMsgs[0], gotErrs[0])
			}

			expectPopulated := !tc.failClose
			if rt.modifySet.Populated() != expectPopulated {
				t.Fatalf("modifySet object tracker's populated field is marked as %v but should be %v", rt.modifySet.Populated(), expectPopulated)
			}
		})
	}
}

func Test_ReadyTracker_TrackAssignImage(t *testing.T) {
	tcs := []struct {
		name            string
		expectedErrMsgs []string
		failClose       bool
	}{
		{
			name:            "TrackAssignImage fail close",
			expectedErrMsgs: []string{"listing AssignImage"},
			failClose:       true,
		},
		{
			name:            "TrackAssignImage fail open",
			expectedErrMsgs: []string{"listing AssignImage"},
			failClose:       false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*mutationsv1alpha1.AssignImageList); ok {
					return fmt.Errorf("Force Test AssignImageList Failure")
				}
				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(&testAssignImage).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, true, false, false, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})
			errChan := make(chan error)
			errGatheringDone := make(chan struct{})

			errs := syncutil.NewConcurrentErrorSlice()
			go func() {
				defer close(errGatheringDone)
				for {
					err, ok := <-errChan
					if !ok {
						return
					}
					errs = errs.Append(err)
				}
			}()

			ctx := context.Background()
			rt.trackAssignImage(ctx, errChan)
			close(errChan)
			<-errGatheringDone

			gotErrs := errs.GetSlice()
			if len(gotErrs) != 1 {
				t.Errorf("unexpected number of errors returned: %+v, expected: 1, got: %v ", gotErrs, len(gotErrs))
			}

			if !strings.Contains(gotErrs[0].Error(), tc.expectedErrMsgs[0]) {
				t.Errorf("expected error to contain %v, but got error: %v ", tc.expectedErrMsgs[0], gotErrs[0])
			}

			expectPopulated := !tc.failClose
			if rt.assignImage.Populated() != expectPopulated {
				t.Fatalf("assignImage object tracker's populated field is marked as %v but should be %v", rt.assignImage.Populated(), expectPopulated)
			}
		})
	}
}

func Test_ReadyTracker_TrackExternalDataProvider(t *testing.T) {
	tcs := []struct {
		name            string
		expectedErrMsgs []string
		failClose       bool
	}{
		{
			name:            "TrackExternalDataProvider fail close",
			expectedErrMsgs: []string{"listing Provider"},
			failClose:       true,
		},
		{
			name:            "TrackExternalDataProvider fail open",
			expectedErrMsgs: []string{"listing Provider"},
			failClose:       false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*externaldatav1beta1.ProviderList); ok {
					return fmt.Errorf("Force Test ProviderList Failure")
				}
				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(&testExternalDataProvider).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, false, true, false, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})
			errChan := make(chan error)
			errGatheringDone := make(chan struct{})

			errs := syncutil.NewConcurrentErrorSlice()
			go func() {
				defer close(errGatheringDone)
				for {
					err, ok := <-errChan
					if !ok {
						return
					}
					errs = errs.Append(err)
				}
			}()

			ctx := context.Background()
			rt.trackExternalDataProvider(ctx, errChan)
			close(errChan)
			<-errGatheringDone

			gotErrs := errs.GetSlice()
			if len(gotErrs) != 1 {
				t.Errorf("unexpected number of errors returned: %+v, expected: 1, got: %v ", gotErrs, len(gotErrs))
			}

			if !strings.Contains(gotErrs[0].Error(), tc.expectedErrMsgs[0]) {
				t.Errorf("expected error to contain %v, but got error: %v ", tc.expectedErrMsgs[0], gotErrs[0])
			}

			expectPopulated := !tc.failClose
			if rt.externalDataProvider.Populated() != expectPopulated {
				t.Fatalf("externalDataProvider object tracker's populated field is marked as %v but should be %v", rt.externalDataProvider.Populated(), expectPopulated)
			}
		})
	}
}

func Test_ReadyTracker_TrackExpansionTemplates(t *testing.T) {
	tcs := []struct {
		name            string
		expectedErrMsgs []string
		failClose       bool
	}{
		{
			name:            "TrackExpansionTemplates fail close",
			expectedErrMsgs: []string{"listing ExpansionTemplates"},
			failClose:       true,
		},
		{
			name:            "TrackExpansionTemplates fail open",
			expectedErrMsgs: []string{"listing ExpansionTemplates"},
			failClose:       false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*expansionv1alpha1.ExpansionTemplateList); ok {
					return fmt.Errorf("Force Test ExpansionTemplateList Failure")
				}
				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(&testExpansionTemplate).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, false, false, true, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})
			errChan := make(chan error)
			errGatheringDone := make(chan struct{})

			errs := syncutil.NewConcurrentErrorSlice()
			go func() {
				defer close(errGatheringDone)
				for {
					err, ok := <-errChan
					if !ok {
						return
					}
					errs = errs.Append(err)
				}
			}()

			ctx := context.Background()
			rt.trackExpansionTemplates(ctx, errChan)
			close(errChan)
			<-errGatheringDone

			gotErrs := errs.GetSlice()
			if len(gotErrs) != 1 {
				t.Errorf("unexpected number of errors returned: %+v, expected: 1, got: %v ", gotErrs, len(gotErrs))
			}

			if !strings.Contains(gotErrs[0].Error(), tc.expectedErrMsgs[0]) {
				t.Errorf("expected error to contain %v, but got error: %v ", tc.expectedErrMsgs[0], gotErrs[0])
			}

			expectPopulated := !tc.failClose
			if rt.expansions.Populated() != expectPopulated {
				t.Fatalf("expansions object tracker's populated field is marked as %v but should be %v", rt.expansions.Populated(), expectPopulated)
			}
		})
	}
}

func Test_ReadyTracker_TrackConstraintTemplates(t *testing.T) {
	tcs := []struct {
		name            string
		expectedErrMsgs []string
		failClose       bool
	}{
		{
			name:            "TrackConstraintTemplates fail close",
			expectedErrMsgs: []string{"listing templates"},
			failClose:       true,
		},
		{
			name:            "TrackConstraintTemplates fail open",
			expectedErrMsgs: []string{"listing templates"},
			failClose:       false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*v1beta1.ConstraintTemplateList); ok {
					return fmt.Errorf("Force Test ConstraintTemplateList Failure")
				}
				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(convertedTemplate.DeepCopyObject()).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, false, false, false, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})
			errChan := make(chan error)
			errGatheringDone := make(chan struct{})

			errs := syncutil.NewConcurrentErrorSlice()
			go func() {
				defer close(errGatheringDone)
				for {
					err, ok := <-errChan
					if !ok {
						return
					}
					errs = errs.Append(err)
				}
			}()

			ctx := context.Background()
			rt.constraintTrackers = syncutil.NewSingleRunner(errChan)
			rt.trackConstraintTemplates(ctx, errChan)
			close(errChan)
			<-errGatheringDone

			gotErrs := errs.GetSlice()
			if len(gotErrs) != 1 {
				t.Errorf("unexpected number of errors returned: %+v, expected: 1, got: %v ", gotErrs, len(gotErrs))
			}

			if !strings.Contains(gotErrs[0].Error(), tc.expectedErrMsgs[0]) {
				t.Errorf("expected error to contain %v, but got error: %v ", tc.expectedErrMsgs[0], gotErrs[0])
			}

			expectPopulated := !tc.failClose
			if rt.templates.Populated() != expectPopulated {
				t.Fatalf("templates object tracker's populated field is marked as %v but should be %v", rt.templates.Populated(), expectPopulated)
			}
		})
	}
}

func Test_ReadyTracker_TrackConfigAndSyncSets(t *testing.T) {
	tcs := []struct {
		name            string
		configForceErr  bool
		syncsetForceErr bool
		expectedErrMsgs []string
		failClose       bool
	}{
		{
			name:            "TrackConfigAndSyncSets config err fail close",
			configForceErr:  true,
			expectedErrMsgs: []string{"listing configs"},
			failClose:       true,
		},
		{
			name:            "TrackConfigAndSyncSets config err fail open",
			configForceErr:  true,
			expectedErrMsgs: []string{"listing configs"},
			failClose:       false,
		},
		{
			name:            "TrackConfigAndSyncSets syncset err fail close",
			syncsetForceErr: true,
			expectedErrMsgs: []string{"listing syncsets"},
			failClose:       true,
		},
		{
			name:            "TrackConfigAndSyncSets syncset err fail open",
			expectedErrMsgs: []string{"listing syncsets"},
			syncsetForceErr: true,
			failClose:       false,
		},
		{
			name:            "TrackConfigAndSyncSets both err fail close",
			expectedErrMsgs: []string{"listing configs", "listing syncsets"},
			configForceErr:  true,
			syncsetForceErr: true,
			failClose:       true,
		},
		{
			name:            "TrackConfigAndSyncSets both err fail open",
			expectedErrMsgs: []string{"listing configs", "listing syncsets"},
			configForceErr:  true,
			syncsetForceErr: true,
			failClose:       false,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*configv1alpha1.ConfigList); ok && tc.configForceErr {
					return fmt.Errorf("Force Test ConfigList Failure")
				}

				if _, ok := list.(*syncsetv1alpha1.SyncSetList); ok && tc.syncsetForceErr {
					return fmt.Errorf("Force Test SyncSetList Failure")
				}

				return client.List(ctx, list, opts...)
			}
			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(&testSyncSet, &testConfig).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, false, false, false, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})
			errChan := make(chan error)
			errGatheringDone := make(chan struct{})

			errs := syncutil.NewConcurrentErrorSlice()
			go func() {
				defer close(errGatheringDone)
				for {
					err, ok := <-errChan
					if !ok {
						return
					}
					errs = errs.Append(err)
				}
			}()

			ctx := context.Background()
			rt.dataTrackers = syncutil.NewSingleRunner(errChan)
			rt.trackConfigAndSyncSets(ctx, errChan)
			close(errChan)
			<-errGatheringDone

			gotErrs := errs.GetSlice()
			if len(gotErrs) != len(tc.expectedErrMsgs) {
				t.Errorf("unexpected number of errors returned: %+v, expected: %v, got: %v ", gotErrs, len(tc.expectedErrMsgs), len(gotErrs))
			}

			for _, expectedErrMsg := range tc.expectedErrMsgs {
				match := false
				for i, err := range gotErrs {
					if strings.Contains(err.Error(), expectedErrMsg) {
						match = true
						gotErrs = append(gotErrs[:i], gotErrs[i+1:]...)
						break
					}
				}
				if !match {
					t.Errorf("expected to get an error that contains: %v, but found none", expectedErrMsg)
				}
			}

			for _, err := range gotErrs {
				t.Errorf("got unexpected error %v", err)
			}

			if tc.failClose {
				expectPopulated := !tc.configForceErr && !tc.syncsetForceErr
				if rt.config.Populated() != expectPopulated || rt.syncsets.Populated() != expectPopulated {
					t.Fatalf("config & syncset object trackers' populated fields are marked as config: %v & syncset: %v, but both should be %v", rt.config.Populated(), rt.syncsets.Populated(), expectPopulated)
				}
			} else if !rt.config.Populated() || !rt.syncsets.Populated() {
				t.Fatalf("config & syncset object trackers' populated fields are marked as config: %v & syncset: %v, but both should be true", rt.config.Populated(), rt.syncsets.Populated())
			}
		})
	}
}

func Test_ReadyTracker_TrackConstraint(t *testing.T) {
	tcs := []struct {
		name            string
		expectedErrMsgs []string
		failClose       bool
	}{
		{
			name:            "TrackConstraint fail close",
			expectedErrMsgs: []string{"listing constraints"},
			failClose:       true,
		},
		{
			name:            "TrackConstraint fail open",
			expectedErrMsgs: []string{"listing constraints"},
			failClose:       false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if v, ok := list.(*unstructured.UnstructuredList); ok && v.GroupVersionKind().Group == "constraints.gatekeeper.sh" {
					return fmt.Errorf("Force Test constraint list Failure")
				}
				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(getTestConstraint()).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, false, false, false, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})
			errChan := make(chan error)
			errGatheringDone := make(chan struct{})

			errs := syncutil.NewConcurrentErrorSlice()
			go func() {
				defer close(errGatheringDone)
				for {
					err, ok := <-errChan
					if !ok {
						return
					}
					errs = errs.Append(err)
				}
			}()

			ctx := context.Background()
			gvk := schema.GroupVersionKind{
				Group:   constraintGroup,
				Version: v1beta1.SchemeGroupVersion.Version,
				Kind:    "FooKind",
			}
			ot := rt.constraints.Get(gvk)
			rt.makeConstraintTrackerFor(gvk, ot)(ctx, errChan)
			close(errChan)
			<-errGatheringDone

			gotErrs := errs.GetSlice()
			if len(gotErrs) != 1 {
				t.Errorf("unexpected number of errors returned: %+v, expected: 1, got: %v ", gotErrs, len(gotErrs))
			}

			if !strings.Contains(gotErrs[0].Error(), tc.expectedErrMsgs[0]) {
				t.Errorf("expected error to contain %v, but got error: %v ", tc.expectedErrMsgs[0], gotErrs[0])
			}

			expectPopulated := !tc.failClose
			if rt.constraints.Populated() != expectPopulated {
				t.Fatalf("constraints object tracker's populated field is marked as %v but should be %v", rt.constraints.Populated(), expectPopulated)
			}
		})
	}
}

func Test_ReadyTracker_TrackData(t *testing.T) {
	tcs := []struct {
		name            string
		expectedErrMsgs []string
		failClose       bool
	}{
		{
			name:            "TrackData fail close",
			expectedErrMsgs: []string{"listing data"},
			failClose:       true,
		},
		{
			name:            "TrackData fail open",
			expectedErrMsgs: []string{"listing data"},
			failClose:       false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if v, ok := list.(*unstructured.UnstructuredList); ok && v.GroupVersionKind().Kind == "PodList" {
					return fmt.Errorf("Force Test data list Failure")
				}

				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(fakes.UnstructuredFor(podGVK, "", "pod1-name")).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, false, false, false, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})
			errChan := make(chan error)
			errGatheringDone := make(chan struct{})

			errs := syncutil.NewConcurrentErrorSlice()
			go func() {
				defer close(errGatheringDone)
				for {
					err, ok := <-errChan
					if !ok {
						return
					}
					errs = errs.Append(err)
				}
			}()

			ctx := context.Background()
			gvk := testSyncSet.Spec.GVKs[0].ToGroupVersionKind()
			dt := rt.data.Get(gvk)

			rt.makeDataTrackerFor(gvk, dt)(ctx, errChan)
			close(errChan)
			<-errGatheringDone

			gotErrs := errs.GetSlice()
			if len(gotErrs) != 1 {
				t.Errorf("unexpected number of errors returned: %+v, expected: 1, got: %v ", gotErrs, len(gotErrs))
			}

			if !strings.Contains(gotErrs[0].Error(), tc.expectedErrMsgs[0]) {
				t.Errorf("expected error to contain %v, but got error: %v ", tc.expectedErrMsgs[0], gotErrs[0])
			}

			expectPopulated := !tc.failClose
			if rt.data.Populated() != expectPopulated {
				t.Fatalf("data object tracker's populated field is marked as %v but should be %v", rt.data.Populated(), expectPopulated)
			}
		})
	}
}

func Test_ReadyTracker_Run_GRP_Wait(t *testing.T) {
	tcs := []struct {
		name       string
		errMessage string
		failClose  bool
	}{
		{
			name:       "Ready Tracker Run GRP.Wait() fail close",
			errMessage: "listing templates",
			failClose:  true,
		},
		{
			name:      "Ready Tracker Run GRP.Wait() fail open",
			failClose: false,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var m sync.Mutex
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*v1beta1.ConstraintTemplateList); ok {
					return fmt.Errorf("Force Test ConstraintTemplateList Failure")
				}

				// Adding a mutex lock here avoids the race condition within fake client's List method
				m.Lock()
				defer m.Unlock()
				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(&testExpansionTemplate, convertedTemplate.DeepCopyObject(), getTestConstraint(), &testSyncSet, fakes.UnstructuredFor(podGVK, "", "pod1-name")).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, false, false, true, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})

			// Run kicks off all the tracking
			ctx, cancel := context.WithCancel(context.Background())
			err := rt.Run(ctx)
			cancel()
			expectError := tc.failClose
			gotError := (err != nil)
			if gotError != expectError || gotError && !strings.Contains(err.Error(), tc.errMessage) {
				t.Fatalf("Run should have returned an error with %v, but got %v", tc.errMessage, err)
			}

			expectPopulated := !tc.failClose
			if rt.Populated() != expectPopulated {
				t.Fatalf("ready tracker's populated field is marked as %v but should be %v", rt.templates.Populated(), expectPopulated)
			}
		})
	}
}

func Test_ReadyTracker_Run_ConstraintTrackers_Wait(t *testing.T) {
	tcs := []struct {
		name       string
		errMessage string
		failClose  bool
	}{
		{
			name:       "Ready Tracker Run GRP.Wait() fail close",
			errMessage: "listing constraints",
			failClose:  true,
		},
		{
			name:      "Ready Tracker Run GRP.Wait() fail open",
			failClose: false,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var m sync.Mutex
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if v, ok := list.(*unstructured.UnstructuredList); ok && v.GroupVersionKind().Group == "constraints.gatekeeper.sh" {
					t.Log(v.GroupVersionKind())
					return fmt.Errorf("Force Test constraint list Failure")
				}
				m.Lock()
				defer m.Unlock()
				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(&testExpansionTemplate, convertedTemplate.DeepCopyObject(), getTestConstraint(), &testSyncSet, fakes.UnstructuredFor(podGVK, "", "pod1-name")).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, false, false, true, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})

			// Run kicks off all the tracking
			ctx, cancel := context.WithCancel(context.Background())
			err := rt.Run(ctx)
			cancel()
			expectError := tc.failClose
			gotError := (err != nil)
			t.Logf("WOWZA: %v", err)
			if gotError != expectError || gotError && !strings.Contains(err.Error(), tc.errMessage) {
				t.Fatalf("Run should have returned an error with %v, but got %v", tc.errMessage, err)
			}

			expectPopulated := !tc.failClose
			if rt.Populated() != expectPopulated {
				t.Fatalf("ready tracker's populated field is marked as %v but should be %v", rt.templates.Populated(), expectPopulated)
			}
		})
	}
}

func Test_ReadyTracker_Run_DataTrackers_Wait(t *testing.T) {
	tcs := []struct {
		name       string
		errMessage string
		failClose  bool
	}{
		{
			name:       "Ready Tracker Run GRP.Wait() fail close",
			errMessage: "listing data",
			failClose:  true,
		},
		{
			name:      "Ready Tracker Run GRP.Wait() fail open",
			failClose: false,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var m sync.Mutex
			funcs := &interceptor.Funcs{}
			funcs.List = func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if v, ok := list.(*unstructured.UnstructuredList); ok && v.GroupVersionKind().Kind == "PodList" {
					return fmt.Errorf("Force Test pod list Failure")
				}
				m.Lock()
				defer m.Unlock()
				return client.List(ctx, list, opts...)
			}

			lister := fake.NewClientBuilder().WithScheme(mustInitializeScheme(runtime.NewScheme())).WithRuntimeObjects(&testExpansionTemplate, convertedTemplate.DeepCopyObject(), getTestConstraint(), &testSyncSet, fakes.UnstructuredFor(podGVK, "", "pod1-name")).WithInterceptorFuncs(*funcs).Build()
			rt := newTracker(lister, false, false, true, tc.failClose, retryNone, func() objData {
				return objData{retries: 0}
			})

			// Run kicks off all the tracking
			ctx, cancel := context.WithCancel(context.Background())
			err := rt.Run(ctx)
			cancel()
			expectError := tc.failClose
			gotError := (err != nil)
			if gotError != expectError || gotError && !strings.Contains(err.Error(), tc.errMessage) {
				t.Fatalf("Run should have returned an error with %v, but got %v", tc.errMessage, err)
			}

			expectPopulated := !tc.failClose
			if rt.Populated() != expectPopulated {
				t.Fatalf("ready tracker's populated field is marked as %v but should be %v", rt.templates.Populated(), expectPopulated)
			}
		})
	}
}
