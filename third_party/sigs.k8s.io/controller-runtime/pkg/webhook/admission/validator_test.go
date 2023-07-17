/*
Copyright 2021 The Kubernetes Authors.

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

package admission

import (
	"context"
	"errors"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
)

var fakeValidatorVK = schema.GroupVersionKind{Group: "foo.test.org", Version: "v1", Kind: "fakeValidator"}

var _ = Describe("validatingHandler", func() {

	decoder := NewDecoder(scheme.Scheme)

	Context("when dealing with successful results without warning", func() {
		f := &fakeValidator{ErrorToReturn: nil, GVKToReturn: fakeValidatorVK, WarningsToReturn: nil}
		handler := validatingHandler{validator: f, decoder: decoder}

		It("should return 200 in response when create succeeds", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})

			Expect(response.Allowed).Should(BeTrue())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusOK)))
		})

		It("should return 200 in response when update succeeds", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})
			Expect(response.Allowed).Should(BeTrue())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusOK)))
		})

		It("should return 200 in response when delete succeeds", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Delete,
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})
			Expect(response.Allowed).Should(BeTrue())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusOK)))
		})
	})

	const warningMessage = "warning message"
	const anotherWarningMessage = "another warning message"
	Context("when dealing with successful results with warning", func() {
		f := &fakeValidator{ErrorToReturn: nil, GVKToReturn: fakeValidatorVK, WarningsToReturn: []string{
			warningMessage,
			anotherWarningMessage,
		}}
		handler := validatingHandler{validator: f, decoder: decoder}

		It("should return 200 in response when create succeeds, with warning messages", func() {
			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})

			Expect(response.Allowed).Should(BeTrue())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusOK)))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(warningMessage))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(anotherWarningMessage))
		})

		It("should return 200 in response when update succeeds, with warning messages", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})
			Expect(response.Allowed).Should(BeTrue())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusOK)))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(warningMessage))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(anotherWarningMessage))
		})

		It("should return 200 in response when delete succeeds, with warning messages", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Delete,
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})
			Expect(response.Allowed).Should(BeTrue())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusOK)))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(warningMessage))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(anotherWarningMessage))
		})
	})

	Context("when dealing with Status errors, with warning messages", func() {
		// Status error would overwrite the warning messages, so no warning messages should be observed.
		expectedError := &apierrors.StatusError{
			ErrStatus: metav1.Status{
				Message: "some message",
				Code:    http.StatusUnprocessableEntity,
			},
		}
		f := &fakeValidator{ErrorToReturn: expectedError, GVKToReturn: fakeValidatorVK, WarningsToReturn: []string{warningMessage, anotherWarningMessage}}
		handler := validatingHandler{validator: f, decoder: decoder}

		It("should propagate the Status from ValidateCreate's return value to the HTTP response", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})

			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(expectedError.Status().Code))
			Expect(*response.Result).Should(Equal(expectedError.Status()))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElements(warningMessage))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElements(anotherWarningMessage))

		})

		It("should propagate the Status from ValidateUpdate's return value to the HTTP response", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})

			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(expectedError.Status().Code))
			Expect(*response.Result).Should(Equal(expectedError.Status()))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElements(warningMessage))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElements(anotherWarningMessage))

		})

		It("should propagate the Status from ValidateDelete's return value to the HTTP response", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Delete,
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})

			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(expectedError.Status().Code))
			Expect(*response.Result).Should(Equal(expectedError.Status()))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElements(warningMessage))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElements(anotherWarningMessage))

		})

	})

	Context("when dealing with Status errors, without warning messages", func() {

		expectedError := &apierrors.StatusError{
			ErrStatus: metav1.Status{
				Message: "some message",
				Code:    http.StatusUnprocessableEntity,
			},
		}
		f := &fakeValidator{ErrorToReturn: expectedError, GVKToReturn: fakeValidatorVK, WarningsToReturn: nil}
		handler := validatingHandler{validator: f, decoder: decoder}

		It("should propagate the Status from ValidateCreate's return value to the HTTP response", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})

			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(expectedError.Status().Code))
			Expect(*response.Result).Should(Equal(expectedError.Status()))

		})

		It("should propagate the Status from ValidateUpdate's return value to the HTTP response", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})

			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(expectedError.Status().Code))
			Expect(*response.Result).Should(Equal(expectedError.Status()))

		})

		It("should propagate the Status from ValidateDelete's return value to the HTTP response", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Delete,
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})

			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(expectedError.Status().Code))
			Expect(*response.Result).Should(Equal(expectedError.Status()))

		})

	})

	Context("when dealing with non-status errors, without warning messages", func() {

		expectedError := errors.New("some error")
		f := &fakeValidator{ErrorToReturn: expectedError, GVKToReturn: fakeValidatorVK}
		handler := validatingHandler{validator: f, decoder: decoder}

		It("should return 403 response when ValidateCreate with error message embedded", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})
			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusForbidden)))
			Expect(response.Result.Reason).Should(Equal(metav1.StatusReasonForbidden))
			Expect(response.Result.Message).Should(Equal(expectedError.Error()))

		})

		It("should return 403 response when ValidateUpdate returns non-APIStatus error", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})
			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusForbidden)))
			Expect(response.Result.Reason).Should(Equal(metav1.StatusReasonForbidden))
			Expect(response.Result.Message).Should(Equal(expectedError.Error()))

		})

		It("should return 403 response when ValidateDelete returns non-APIStatus error", func() {
			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Delete,
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})
			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusForbidden)))
			Expect(response.Result.Reason).Should(Equal(metav1.StatusReasonForbidden))
			Expect(response.Result.Message).Should(Equal(expectedError.Error()))
		})
	})

	Context("when dealing with non-status errors, with warning messages", func() {

		expectedError := errors.New("some error")
		f := &fakeValidator{ErrorToReturn: expectedError, GVKToReturn: fakeValidatorVK, WarningsToReturn: []string{warningMessage, anotherWarningMessage}}
		handler := validatingHandler{validator: f, decoder: decoder}

		It("should return 403 response when ValidateCreate with error message embedded", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})
			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusForbidden)))
			Expect(response.Result.Reason).Should(Equal(metav1.StatusReasonForbidden))
			Expect(response.Result.Message).Should(Equal(expectedError.Error()))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(warningMessage))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(anotherWarningMessage))
		})

		It("should return 403 response when ValidateUpdate returns non-APIStatus error", func() {

			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})
			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusForbidden)))
			Expect(response.Result.Reason).Should(Equal(metav1.StatusReasonForbidden))
			Expect(response.Result.Message).Should(Equal(expectedError.Error()))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(warningMessage))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(anotherWarningMessage))

		})

		It("should return 403 response when ValidateDelete returns non-APIStatus error", func() {
			response := handler.Handle(context.TODO(), Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Delete,
					OldObject: runtime.RawExtension{
						Raw:    []byte("{}"),
						Object: handler.validator,
					},
				},
			})
			Expect(response.Allowed).Should(BeFalse())
			Expect(response.Result.Code).Should(Equal(int32(http.StatusForbidden)))
			Expect(response.Result.Reason).Should(Equal(metav1.StatusReasonForbidden))
			Expect(response.Result.Message).Should(Equal(expectedError.Error()))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(warningMessage))
			Expect(response.AdmissionResponse.Warnings).Should(ContainElement(anotherWarningMessage))

		})
	})

	PIt("should return 400 in response when create fails on decode", func() {})

	PIt("should return 400 in response when update fails on decoding new object", func() {})

	PIt("should return 400 in response when update fails on decoding old object", func() {})

	PIt("should return 400 in response when delete fails on decode", func() {})

})

// fakeValidator provides fake validating webhook functionality for testing
// It implements the admission.Validator interface and
// rejects all requests with the same configured error
// or passes if ErrorToReturn is nil.
// And it would always return configured warning messages WarningsToReturn.
type fakeValidator struct {
	// ErrorToReturn is the error for which the fakeValidator rejects all requests
	ErrorToReturn error `json:"errorToReturn,omitempty"`
	// GVKToReturn is the GroupVersionKind that the webhook operates on
	GVKToReturn schema.GroupVersionKind
	// WarningsToReturn is the warnings for fakeValidator returns to all requests
	WarningsToReturn []string
}

func (v *fakeValidator) ValidateCreate() (warnings Warnings, err error) {
	return v.WarningsToReturn, v.ErrorToReturn
}

func (v *fakeValidator) ValidateUpdate(old runtime.Object) (warnings Warnings, err error) {
	return v.WarningsToReturn, v.ErrorToReturn
}

func (v *fakeValidator) ValidateDelete() (warnings Warnings, err error) {
	return v.WarningsToReturn, v.ErrorToReturn
}

func (v *fakeValidator) SetGroupVersionKind(gvk schema.GroupVersionKind) {
	v.GVKToReturn = gvk
}

func (v *fakeValidator) GroupVersionKind() schema.GroupVersionKind {
	return v.GVKToReturn
}

func (v *fakeValidator) GetObjectKind() schema.ObjectKind {
	return v
}

func (v *fakeValidator) DeepCopyObject() runtime.Object {
	return &fakeValidator{
		ErrorToReturn:    v.ErrorToReturn,
		GVKToReturn:      v.GVKToReturn,
		WarningsToReturn: v.WarningsToReturn,
	}
}
