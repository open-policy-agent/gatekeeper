package schema

import (
	"fmt"
	"strings"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	admissionv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/policy/validating"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
)

const (
	// Name is the name of the driver.
	Name = "K8sNativeValidation"
	// ReservedPrefix signifies a prefix that no user-defined value (variable, matcher, etc.) is allowed to have.
	// This gives us the ability to add new variables in the future without worrying about breaking pre-existing templates.
	ReservedPrefix = "gatekeeper_internal_"
	// ParamsName is the VAP variable constraint parameters will be bound to.
	ParamsName = "params"
	// ObjectName is the VAP variable that describes either an object or (on DELETE requests) oldObject.
	ObjectName = "anyObject"
)

type Validation struct {
	// A CEL expression. Maps to ValidationAdmissionPolicy's `spec.validations`.
	Expression        string `json:"expression,omitempty"`
	Message           string `json:"message,omitempty"`
	MessageExpression string `json:"messageExpression,omitempty"`
}

type MatchCondition struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
}

type Variable struct {
	// A CEL variable definition. Maps to ValidationAdmissionPolicy's `spec.variables`.
	Name       string `json:"name,omitempty"`
	Expression string `json:"expression,omitempty"`
}

type Source struct {
	// Validations maps to ValidatingAdmissionPolicy's `spec.validations`.
	Validations []Validation `json:"validations,omitempty"`

	// FailurePolicy maps to ValidatingAdmissionPolicy's `spec.failurePolicy`.
	FailurePolicy *string `json:"failurePolicy,omitempty"`

	// MatchConditions maps to ValidatingAdmissionPolicy's `spec.matchConditions`.
	MatchConditions []MatchCondition `json:"matchCondition,omitempty"`

	// Variables maps to ValidatingAdmissionPolicy's `spec.variables`.
	Variables []Variable `json:"variables,omitempty"`

	// GenerateVAP enables/disables VAP generation and enforcement for policy.
	GenerateVAP *bool `json:"generateVAP,omitempty"`
}

func (in *Source) Validate() error {
	if err := in.validateMatchConditions(); err != nil {
		return err
	}
	if err := in.validateVariables(); err != nil {
		return err
	}
	if _, err := in.GetFailurePolicy(); err != nil {
		return err
	}

	return nil
}

func (in *Source) validateMatchConditions() error {
	for _, condition := range in.MatchConditions {
		if strings.HasPrefix(condition.Name, ReservedPrefix) {
			return fmt.Errorf("%w: %s is not a valid match condition; cannot have %q as a prefix", ErrBadMatchCondition, condition.Name, ReservedPrefix)
		}
	}
	return nil
}

func (in *Source) GetMatchConditions() ([]cel.ExpressionAccessor, error) {
	if err := in.validateMatchConditions(); err != nil {
		return nil, err
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

func (in *Source) GetV1Beta1MatchConditions() ([]admissionv1beta1.MatchCondition, error) {
	if err := in.validateMatchConditions(); err != nil {
		return nil, err
	}

	var matchConditions []admissionv1beta1.MatchCondition
	for _, mc := range in.MatchConditions {
		matchConditions = append(matchConditions, admissionv1beta1.MatchCondition{
			Name:       mc.Name,
			Expression: mc.Expression,
		})
	}
	return matchConditions, nil
}

func (in *Source) validateVariables() error {
	for _, v := range in.Variables {
		if strings.HasPrefix(v.Name, ReservedPrefix) {
			return fmt.Errorf("%w: %s is not a valid variable; cannot have %q as a prefix", ErrBadVariable, v.Name, ReservedPrefix)
		}
		if v.Name == ParamsName {
			return fmt.Errorf("%w: %s an invalid variable name, %q is a reserved keyword", ErrBadVariable, ParamsName, ParamsName)
		}
		if v.Name == ObjectName {
			return fmt.Errorf("%w: %s an invalid variable name, %q is a reserved keyword", ErrBadVariable, ObjectName, ObjectName)
		}
	}
	return nil
}

func (in *Source) GetVariables() ([]cel.NamedExpressionAccessor, error) {
	if err := in.validateVariables(); err != nil {
		return nil, err
	}

	vars := make([]cel.NamedExpressionAccessor, len(in.Variables))
	for i, v := range in.Variables {
		vars[i] = &validating.Variable{
			Name:       v.Name,
			Expression: v.Expression,
		}
	}

	return vars, nil
}

func (in *Source) GetV1Beta1Variables() ([]admissionv1beta1.Variable, error) {
	if err := in.validateVariables(); err != nil {
		return nil, err
	}

	var variables []admissionv1beta1.Variable
	for _, v := range in.Variables {
		variables = append(variables, admissionv1beta1.Variable{
			Name:       v.Name,
			Expression: v.Expression,
		})
	}
	return variables, nil
}

func (in *Source) GetValidations() ([]cel.ExpressionAccessor, error) {
	validations := make([]cel.ExpressionAccessor, len(in.Validations))
	for i, validation := range in.Validations {
		celValidation := validating.ValidationCondition{
			Expression: validation.Expression,
			Message:    validation.Message,
		}
		validations[i] = &celValidation
	}
	return validations, nil
}

func (in *Source) GetV1Beta1Validatons() ([]admissionv1beta1.Validation, error) {
	var validations []admissionv1beta1.Validation
	for _, v := range in.Validations {
		validations = append(validations, admissionv1beta1.Validation{
			Expression:        v.Expression,
			Message:           v.Message,
			MessageExpression: v.MessageExpression,
		})
	}
	return validations, nil
}

func (in *Source) GetMessageExpressions() ([]cel.ExpressionAccessor, error) {
	messageExpressions := make([]cel.ExpressionAccessor, len(in.Validations))
	for i, validation := range in.Validations {
		if validation.MessageExpression != "" {
			condition := validating.MessageExpressionCondition{
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
		return nil, fmt.Errorf("%w: unrecognized failure policy: %s", ErrBadFailurePolicy, *in.FailurePolicy)
	}

	return &out, nil
}

func (in *Source) GetV1Beta1FailurePolicy() (*admissionv1beta1.FailurePolicyType, error) {
	var out admissionv1beta1.FailurePolicyType
	/// TODO(ritazh): default for now until the feature is safe to fail close
	if in.FailurePolicy == nil {
		out = admissionv1beta1.Ignore
		return &out, nil
	}

	switch *in.FailurePolicy {
	case string(admissionv1.Fail):
		out = admissionv1beta1.Fail
	case string(admissionv1.Ignore):
		out = admissionv1beta1.Ignore
	default:
		return nil, fmt.Errorf("%w: unrecognized failure policy: %s", ErrBadFailurePolicy, *in.FailurePolicy)
	}

	return &out, nil
}

// ToUnstructured() is a convenience method for converting to unstructured.
// Intended for testing. It will panic on error.
func (in *Source) MustToUnstructured() map[string]interface{} {
	if in == nil {
		return nil
	}

	out, err := runtime.DefaultUnstructuredConverter.ToUnstructured(in)
	if err != nil {
		panic(fmt.Errorf("cannot cast as unstructured: %w", err))
	}

	return out
}

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

func GetSourceFromTemplate(ct *templates.ConstraintTemplate) (*Source, error) {
	if len(ct.Spec.Targets) != 1 {
		return nil, ErrOneTargetAllowed
	}

	var source *Source
	for _, code := range ct.Spec.Targets[0].Code {
		if code.Engine != Name {
			continue
		}
		var err error
		source, err = GetSource(code)
		if err != nil {
			return nil, err
		}
		break
	}
	if source == nil {
		return nil, ErrCodeNotDefined
	}
	return source, nil
}
