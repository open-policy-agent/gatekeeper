package constraint

import (
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/open-policy-agent/frameworks/constraint/pkg/apis/templates/v1beta1"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
)

func TestTotalConstraintsCache(t *testing.T) {
	constraintsCache := NewConstraintsCache()
	if len(constraintsCache.cache) != 0 {
		t.Errorf("cache: %v, wanted empty cache", spew.Sdump(constraintsCache.cache))
	}

	constraintsCache.addConstraintKey("test", tags{
		enforcementAction: util.Deny,
		status:            metrics.ActiveStatus,
	})
	if len(constraintsCache.cache) != 1 {
		t.Errorf("cache: %v, wanted cache with 1 element", spew.Sdump(constraintsCache.cache))
	}

	constraintsCache.deleteConstraintKey("test")
	if len(constraintsCache.cache) != 0 {
		t.Errorf("cache: %v, wanted empty cache", spew.Sdump(constraintsCache.cache))
	}
}

func TestHasVAPCel(t *testing.T) {
	ct := &v1beta1.ConstraintTemplate{}

	// Test when the code is empty
	ct.Spec.Targets = []v1beta1.Target{
		{
			Code: []v1beta1.Code{},
		},
	}
	expected := false
	if result := HasVAPCel(ct); result != expected {
		t.Errorf("hasVAPCel() = %v, expected %v", result, expected)
	}

	// Test when the code has only one Rego engine
	ct.Spec.Targets = []v1beta1.Target{
		{
			Code: []v1beta1.Code{
				{
					Engine: "Rego",
				},
			},
		},
	}
	expected = false
	if result := HasVAPCel(ct); result != expected {
		t.Errorf("hasVAPCel() = %v, expected %v", result, expected)
	}

	// Test when the code has multiple engines including Rego
	ct.Spec.Targets = []v1beta1.Target{
		{
			Code: []v1beta1.Code{
				{
					Engine: "Rego",
				},
				{
					Engine: "K8sNativeValidation",
				},
			},
		},
	}
	expected = true
	if result := HasVAPCel(ct); result != expected {
		t.Errorf("hasVAPCel() = %v, expected %v", result, expected)
	}

	// Test when the code has only K8sNativeValidation engine
	ct.Spec.Targets = []v1beta1.Target{
		{
			Code: []v1beta1.Code{
				{
					Engine: "K8sNativeValidation",
				},
			},
		},
	}
	expected = true
	if result := HasVAPCel(ct); result != expected {
		t.Errorf("hasVAPCel() = %v, expected %v", result, expected)
	}
}
