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
	"sync"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	syncsetv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/syncset/v1alpha1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/fakes"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

	podGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
)

// Verify that TryCancelTemplate functions the same as regular CancelTemplate if readinessRetries is set to 0.
func Test_ReadyTracker_TryCancelTemplate_No_Retries(t *testing.T) {
	g := gomega.NewWithT(t)

	l := fakes.NewTestLister(
		fakes.WithConstraintTemplates([]*templates.ConstraintTemplate{&testConstraintTemplate}),
	)
	rt := newTracker(l, false, false, false, func() objData {
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

	g.Eventually(func() bool {
		return rt.Populated()
	}, "10s").Should(gomega.BeTrue())

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
	g := gomega.NewWithT(t)

	l := fakes.NewTestLister(
		fakes.WithConstraintTemplates([]*templates.ConstraintTemplate{&testConstraintTemplate}),
	)
	rt := newTracker(l, false, false, false, func() objData {
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

	g.Eventually(func() bool {
		return rt.Populated()
	}, "10s").Should(gomega.BeTrue())

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
	tcs := []struct {
		name    string
		retries int
	}{
		{name: "no retries", retries: 0},
		{name: "with retries", retries: 2},
	}

	l := fakes.NewTestLister(
		fakes.WithSyncSets([]*syncsetv1alpha1.SyncSet{&testSyncSet}),
	)
	for _, tt := range tcs {
		objDataFn := func() objData {
			return objData{retries: tt.retries}
		}
		rt := newTracker(l, false, false, false, objDataFn)

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
		}, 10*time.Second, 1*time.Second, "waiting for RT to populated")
		require.False(t, rt.Satisfied(), "tracker with retries should not be satisfied")

		// observe the sync source for readiness
		rt.syncsets.Observe(&testSyncSet)

		for i := tt.retries; i > 0; i-- {
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
	}
}
