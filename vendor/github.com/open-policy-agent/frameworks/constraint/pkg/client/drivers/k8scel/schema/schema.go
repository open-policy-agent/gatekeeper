package schema

import (
	"errors"
	"fmt"
	"strings"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/validatingadmissionpolicy"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
)

const (
	// Name is the name of the driver.
	Name           = "K8sNativeValidation"
	ReservedPrefix = "g8r-"
)

var (
	ErrBadType      = errors.New("Could not recognize the type")
	ErrMissingField = errors.New("K8sNativeValidation source missing required field")
)

type Validation struct {
	// A CEL expression. Maps to ValidationAdmissionPolicy's spec.validations.expression
	Expression        string
	Message           string
	MessageExpression string
}

type MatchCondition struct {
	Name string `json:"name"`

	Expression string `json:"expression"`
}

type Source struct {
	// Validations maps to ValidatingAdmissionPolicy's spec.validations.
	Validations []Validation `json:"validations,omitempty"`

	// FailurePolicy maps to ValidatingAdmissionPolicy's spec.failurePolicy
	FailurePolicy *string `json:"failurePolicy,omitempty"`

	// MatchConditions maps to ValidatingAdmissionPolicy's spec.matchConditions
	MatchConditions []MatchCondition `json:"matchCondition,omitempty"`
}

func (in *Source) GetMatchConditions() ([]cel.ExpressionAccessor, error) {
	for _, condition := range in.MatchConditions {
		if strings.HasPrefix(condition.Name, ReservedPrefix) {
			return nil, fmt.Errorf("%s is not a valid match condition; cannot have %q as a prefix", condition.Name, ReservedPrefix)
		}
	}

	matchConditions := make([]cel.ExpressionAccessor, len(in.MatchConditions))
	for i, mc := range in.MatchConditions {
		matchConditions[i] = &matchconditions.MatchCondition{
			Name:       mc.Name,
			Expression: mc.Expression,
		}
	}
	return matchConditions, nil
}

func (in *Source) GetValidations() ([]cel.ExpressionAccessor, error) {
	validations := make([]cel.ExpressionAccessor, len(in.Validations))
	for i, validation := range in.Validations {
		celValidation := validatingadmissionpolicy.ValidationCondition{
			Expression: validation.Expression,
			Message:    validation.Message,
		}
		validations[i] = &celValidation
	}
	return validations, nil
}

func (in *Source) GetMessageExpressions() ([]cel.ExpressionAccessor, error) {
	messageExpressions := make([]cel.ExpressionAccessor, len(in.Validations))
	for i, validation := range in.Validations {
		if validation.MessageExpression != "" {
			condition := validatingadmissionpolicy.MessageExpressionCondition{
				MessageExpression: validation.MessageExpression,
			}
			messageExpressions[i] = &condition
		}
	}
	return messageExpressions, nil
}

func (in *Source) GetFailurePolicy() (*admissionv1.FailurePolicyType, error) {
	if in.FailurePolicy == nil {
		return nil, nil
	}

	var out admissionv1.FailurePolicyType

	switch *in.FailurePolicy {
	case string(admissionv1.Fail):
		out = admissionv1.Fail
	case string(admissionv1.Ignore):
		out = admissionv1.Ignore
	default:
		return nil, fmt.Errorf("unrecognized failure policy: %s", *in.FailurePolicy)
	}

	return &out, nil
}

func (in *Source) Validate() error {
	if _, err := in.GetMatchConditions(); err != nil {
		return err
	}
	if _, err := in.GetFailurePolicy(); err != nil {
		return err
	}
	return nil
}

// ToUnstructured() is a convenience method for converting to unstructured.
// Intended for testing. It will panic on error. TODO: rename to MustToUnstructured()?
func (in *Source) ToUnstructured() map[string]interface{} {
	if in == nil {
		return nil
	}

	out, err := runtime.DefaultUnstructuredConverter.ToUnstructured(in)
	if err != nil {
		panic(fmt.Errorf("cannot cast as unstructured: %w", err))
	}

	return out
}

// TODO: things to validate:
//  * once variables can be defined, disallow `params` as a variable name

func GetSource(code templates.Code) (*Source, error) {
	rawCode := code.Source
	v, ok := rawCode.Value.(map[string]interface{})
	if !ok {
		return nil, ErrBadType
	}

	out := &Source{}

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(v, out); err != nil {
		return nil, err
	}

	if err := out.Validate(); err != nil {
		return nil, err
	}

	return out, nil
}
