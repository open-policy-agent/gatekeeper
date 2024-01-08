package util

import (
	"errors"
	"reflect"
	"testing"

	v1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestSetObjectOnDelete(t *testing.T) {
	testCases := []struct {
		name    string
		req     *admission.Request
		wantErr error
	}{
		{
			name: "request not on delete",
			req: &admission.Request{AdmissionRequest: v1.AdmissionRequest{
				Operation: "CREATE",
			}},
			wantErr: nil,
		},
		{
			name: "err on request and nil oldObject",
			req: &admission.Request{AdmissionRequest: v1.AdmissionRequest{
				Operation: "DELETE",
			}},
			wantErr: ErrOldObjectIsNil,
		},
		{
			name: "handle ok oldObject not nil",
			req: &admission.Request{AdmissionRequest: v1.AdmissionRequest{
				Operation: "DELETE",
				OldObject: runtime.RawExtension{
					Raw: []byte{'a', 'b', 'c'},
				},
			}},
			wantErr: nil,
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := SetObjectOnDelete(tc.req)

			if !errors.Is(tc.wantErr, err) {
				t.Fatalf("error did not match what was expected\n want: %v \n got: %v \n", tc.wantErr, err)
			}

			// open box: make sure that the OldObject field has been copied into the Object field
			if !reflect.DeepEqual(tc.req.AdmissionRequest.OldObject, tc.req.AdmissionRequest.Object) {
				t.Fatal("oldObject and object need to match")
			}
		})
	}
}
