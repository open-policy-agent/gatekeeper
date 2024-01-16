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

package cel

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"time"

	celgo "github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"

	"k8s.io/klog/v2"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/apiextensions-apiserver/pkg/apiserver/schema/cel/model"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/environment"
	"k8s.io/apiserver/pkg/cel/metrics"

	celconfig "k8s.io/apiserver/pkg/apis/cel"
)

// Validator parallels the structure of schema.Structural and includes the compiled CEL programs
// for the x-kubernetes-validations of each schema node.
type Validator struct {
	Items      *Validator
	Properties map[string]Validator

	AdditionalProperties *Validator

	compiledRules []CompilationResult

	// Program compilation is pre-checked at CRD creation/update time, so we don't expect compilation to fail
	// they are recompiled and added to this type, and it does, it is an internal bug.
	// But if somehow we get any compilation errors, we track them and then surface them as validation errors.
	compilationErr error

	// isResourceRoot is true if this validator node is for the root of a resource. Either the root of the
	// custom resource being validated, or the root of an XEmbeddedResource object.
	isResourceRoot bool

	// celActivationFactory produces an Activation, which resolves identifiers (e.g. self and
	// oldSelf) to CEL values.
	celActivationFactory func(sts *schema.Structural, obj, oldObj interface{}) interpreter.Activation
}

// NewValidator returns compiles all the CEL programs defined in x-kubernetes-validations extensions
// of the Structural schema and returns a custom resource validator that contains nested
// validators for all items, properties and additionalProperties that transitively contain validator rules.
// Returns nil if there are no validator rules in the Structural schema. May return a validator containing only errors.
// Adding perCallLimit as input arg for testing purpose only. Callers should always use const PerCallLimit from k8s.io/apiserver/pkg/apis/cel/config.go as input
func NewValidator(s *schema.Structural, isResourceRoot bool, perCallLimit uint64) *Validator {
	if !hasXValidations(s) {
		return nil
	}
	return validator(s, isResourceRoot, model.SchemaDeclType(s, isResourceRoot), perCallLimit)
}

// validator creates a Validator for all x-kubernetes-validations at the level of the provided schema and lower and
// returns the Validator if any x-kubernetes-validations exist in the schema, or nil if no x-kubernetes-validations
// exist. declType is expected to be a CEL DeclType corresponding to the structural schema.
// perCallLimit was added for testing purpose only. Callers should always use const PerCallLimit from k8s.io/apiserver/pkg/apis/cel/config.go as input.
func validator(s *schema.Structural, isResourceRoot bool, declType *cel.DeclType, perCallLimit uint64) *Validator {
	compiledRules, err := Compile(s, declType, perCallLimit, environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()), StoredExpressionsEnvLoader())
	var itemsValidator, additionalPropertiesValidator *Validator
	var propertiesValidators map[string]Validator
	if s.Items != nil {
		itemsValidator = validator(s.Items, s.Items.XEmbeddedResource, declType.ElemType, perCallLimit)
	}
	if len(s.Properties) > 0 {
		propertiesValidators = make(map[string]Validator, len(s.Properties))
		for k, p := range s.Properties {
			prop := p
			var fieldType *cel.DeclType
			if escapedPropName, ok := cel.Escape(k); ok {
				if f, ok := declType.Fields[escapedPropName]; ok {
					fieldType = f.Type
				} else {
					// fields with unknown types are omitted from CEL validation entirely
					continue
				}
			} else {
				// field may be absent from declType if the property name is unescapable, in which case we should convert
				// the field value type to a DeclType.
				fieldType = model.SchemaDeclType(&prop, prop.XEmbeddedResource)
				if fieldType == nil {
					continue
				}
			}
			if p := validator(&prop, prop.XEmbeddedResource, fieldType, perCallLimit); p != nil {
				propertiesValidators[k] = *p
			}
		}
	}
	if s.AdditionalProperties != nil && s.AdditionalProperties.Structural != nil {
		additionalPropertiesValidator = validator(s.AdditionalProperties.Structural, s.AdditionalProperties.Structural.XEmbeddedResource, declType.ElemType, perCallLimit)
	}
	if len(compiledRules) > 0 || err != nil || itemsValidator != nil || additionalPropertiesValidator != nil || len(propertiesValidators) > 0 {
		var activationFactory func(*schema.Structural, interface{}, interface{}) interpreter.Activation = validationActivationWithoutOldSelf
		for _, rule := range compiledRules {
			if rule.TransitionRule {
				activationFactory = validationActivationWithOldSelf
				break
			}
		}

		return &Validator{
			compiledRules:        compiledRules,
			compilationErr:       err,
			isResourceRoot:       isResourceRoot,
			Items:                itemsValidator,
			AdditionalProperties: additionalPropertiesValidator,
			Properties:           propertiesValidators,
			celActivationFactory: activationFactory,
		}
	}

	return nil
}

// Validate validates all x-kubernetes-validations rules in Validator against obj and returns any errors.
// If the validation rules exceed the costBudget, subsequent evaluations will be skipped, the list of errs returned will not be empty, and a negative remainingBudget will be returned.
// Most callers can ignore the returned remainingBudget value unless another validate call is going to be made
// context is passed for supporting context cancellation during cel validation
func (s *Validator) Validate(ctx context.Context, fldPath *field.Path, sts *schema.Structural, obj, oldObj interface{}, costBudget int64) (errs field.ErrorList, remainingBudget int64) {
	t := time.Now()
	defer func() {
		metrics.Metrics.ObserveEvaluation(time.Since(t))
	}()
	remainingBudget = costBudget
	if s == nil || obj == nil {
		return nil, remainingBudget
	}

	errs, remainingBudget = s.validateExpressions(ctx, fldPath, sts, obj, oldObj, remainingBudget)
	if remainingBudget < 0 {
		return errs, remainingBudget
	}
	switch obj := obj.(type) {
	case []interface{}:
		oldArray, _ := oldObj.([]interface{})
		var arrayErrs field.ErrorList
		arrayErrs, remainingBudget = s.validateArray(ctx, fldPath, sts, obj, oldArray, remainingBudget)
		errs = append(errs, arrayErrs...)
		return errs, remainingBudget
	case map[string]interface{}:
		oldMap, _ := oldObj.(map[string]interface{})
		var mapErrs field.ErrorList
		mapErrs, remainingBudget = s.validateMap(ctx, fldPath, sts, obj, oldMap, remainingBudget)
		errs = append(errs, mapErrs...)
		return errs, remainingBudget
	}
	return errs, remainingBudget
}

func (s *Validator) validateExpressions(ctx context.Context, fldPath *field.Path, sts *schema.Structural, obj, oldObj interface{}, costBudget int64) (errs field.ErrorList, remainingBudget int64) {
	// guard against oldObj being a non-nil interface with a nil value
	if oldObj != nil {
		v := reflect.ValueOf(oldObj)
		switch v.Kind() {
		case reflect.Map, reflect.Pointer, reflect.Interface, reflect.Slice:
			if v.IsNil() {
				oldObj = nil // +k8s:verify-mutation:reason=clone
			}
		}
	}

	remainingBudget = costBudget
	if obj == nil {
		// We only validate non-null values. Rules that need to check for the state of a nullable value or the presence of an optional
		// field must do so from the surrounding schema. E.g. if an array has nullable string items, a rule on the array
		// schema can check if items are null, but a rule on the nullable string schema only validates the non-null strings.
		return nil, remainingBudget
	}
	if s.compilationErr != nil {
		errs = append(errs, field.Invalid(fldPath, sts.Type, fmt.Sprintf("rule compiler initialization error: %v", s.compilationErr)))
		return errs, remainingBudget
	}
	if len(s.compiledRules) == 0 {
		return nil, remainingBudget // nothing to do
	}
	if remainingBudget <= 0 {
		errs = append(errs, field.Invalid(fldPath, sts.Type, fmt.Sprintf("validation failed due to running out of cost budget, no further validation rules will be run")))
		return errs, -1
	}
	if s.isResourceRoot {
		sts = model.WithTypeAndObjectMeta(sts)
	}
	activation := s.celActivationFactory(sts, obj, oldObj)
	for i, compiled := range s.compiledRules {
		rule := sts.XValidations[i]
		if compiled.Error != nil {
			errs = append(errs, field.Invalid(fldPath, sts.Type, fmt.Sprintf("rule compile error: %v", compiled.Error)))
			continue
		}
		if compiled.Program == nil {
			// rule is empty
			continue
		}
		if compiled.TransitionRule && oldObj == nil {
			// transition rules are evaluated only if there is a comparable existing value
			continue
		}
		evalResult, evalDetails, err := compiled.Program.ContextEval(ctx, activation)
		if evalDetails == nil {
			errs = append(errs, field.InternalError(fldPath, fmt.Errorf("runtime cost could not be calculated for validation rule: %v, no further validation rules will be run", ruleErrorString(rule))))
			return errs, -1
		} else {
			rtCost := evalDetails.ActualCost()
			if rtCost == nil {
				errs = append(errs, field.Invalid(fldPath, sts.Type, fmt.Sprintf("runtime cost could not be calculated for validation rule: %v, no further validation rules will be run", ruleErrorString(rule))))
				return errs, -1
			} else {
				if *rtCost > math.MaxInt64 || int64(*rtCost) > remainingBudget {
					errs = append(errs, field.Invalid(fldPath, sts.Type, fmt.Sprintf("validation failed due to running out of cost budget, no further validation rules will be run")))
					return errs, -1
				}
				remainingBudget -= int64(*rtCost)
			}
		}
		if err != nil {
			// see types.Err for list of well defined error types
			if strings.HasPrefix(err.Error(), "no such overload") {
				// Most overload errors are caught by the compiler, which provides details on where exactly in the rule
				// error was found. Here, an overload error has occurred at runtime no details are provided, so we
				// append a more descriptive error message. This error can only occur when static type checking has
				// been bypassed. int-or-string is typed as dynamic and so bypasses compiler type checking.
				errs = append(errs, field.Invalid(fldPath, sts.Type, fmt.Sprintf("'%v': call arguments did not match a supported operator, function or macro signature for rule: %v", err, ruleErrorString(rule))))
			} else if strings.HasPrefix(err.Error(), "operation cancelled: actual cost limit exceeded") {
				errs = append(errs, field.Invalid(fldPath, sts.Type, fmt.Sprintf("'%v': no further validation rules will be run due to call cost exceeds limit for rule: %v", err, ruleErrorString(rule))))
				return errs, -1
			} else {
				// no such key: {key}, index out of bounds: {index}, integer overflow, division by zero, ...
				errs = append(errs, field.Invalid(fldPath, sts.Type, fmt.Sprintf("%v evaluating rule: %v", err, ruleErrorString(rule))))
			}
			continue
		}
		if evalResult != types.True {
			if len(compiled.NormalizedRuleFieldPath) > 0 {
				fldPath = fldPath.Child(compiled.NormalizedRuleFieldPath)
			}
			if compiled.MessageExpression != nil {
				messageExpression, newRemainingBudget, msgErr := evalMessageExpression(ctx, compiled.MessageExpression, rule.MessageExpression, activation, remainingBudget)
				if msgErr != nil {
					if msgErr.Type == cel.ErrorTypeInternal {
						errs = append(errs, field.InternalError(fldPath, msgErr))
						return errs, -1
					} else if msgErr.Type == cel.ErrorTypeInvalid {
						errs = append(errs, field.Invalid(fldPath, sts.Type, msgErr.Error()))
						return errs, -1
					} else {
						klog.V(2).ErrorS(msgErr, "messageExpression evaluation failed")
						errs = append(errs, fieldErrorForReason(fldPath, sts.Type, ruleMessageOrDefault(rule), rule.Reason))
						remainingBudget = newRemainingBudget
					}
				} else {
					errs = append(errs, fieldErrorForReason(fldPath, sts.Type, messageExpression, rule.Reason))
					remainingBudget = newRemainingBudget
				}
			} else {
				errs = append(errs, fieldErrorForReason(fldPath, sts.Type, ruleMessageOrDefault(rule), rule.Reason))
			}
		}
	}
	return errs, remainingBudget
}

var unescapeMatcher = regexp.MustCompile(`\\.`)

func unescapeSingleQuote(s string) (string, error) {
	var err error
	unescaped := unescapeMatcher.ReplaceAllStringFunc(s, func(matchStr string) string {
		directive := matchStr[1]
		switch directive {
		case 'a':
			return "\a"
		case 'b':
			return "\b"
		case 'f':
			return "\f"
		case 'n':
			return "\n"
		case 'r':
			return "\r"
		case 't':
			return "\t"
		case 'v':
			return "\v"
		case '\'':
			return "'"
		case '\\':
			return "\\"
		default:
			err = fmt.Errorf("invalid escape char %s", matchStr)
			return ""
		}
	})
	return unescaped, err
}

// ValidFieldPath validates that jsonPath is a valid JSON Path containing only field and map accessors
// that are valid for the given schema, and returns a field.Path representation of the validated jsonPath or an error.
func ValidFieldPath(jsonPath string, schema *schema.Structural) (validFieldPath *field.Path, err error) {
	appendToPath := func(name string, isNamed bool) error {
		if !isNamed {
			validFieldPath = validFieldPath.Key(name)
			schema = schema.AdditionalProperties.Structural
		} else {
			validFieldPath = validFieldPath.Child(name)
			val, ok := schema.Properties[name]
			if !ok {
				return fmt.Errorf("does not refer to a valid field")
			}
			schema = &val
		}
		return nil
	}

	validFieldPath = nil

	scanner := bufio.NewScanner(strings.NewReader(jsonPath))

	// configure the scanner to split the string into tokens.
	// The three delimiters ('.', '[', ']') will be returned as single char tokens.
	// All other text between delimiters is returned as string tokens.
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if len(data) > 0 {
			for i := 0; i < len(data); i++ {
				// If in a single quoted string, look for the end of string
				// ignoring delimiters.
				if data[0] == '\'' {
					if i > 0 && data[i] == '\'' && data[i-1] != '\\' {
						// Return quoted string
						return i + 1, data[:i+1], nil
					}
					continue
				}
				switch data[i] {
				case '.', '[', ']': // delimiters
					if i == 0 {
						// Return the delimiter.
						return 1, data[:1], nil
					} else {
						// Return identifier leading up to the delimiter.
						// The next call to split will return the delimiter.
						return i, data[:i], nil
					}
				}
			}
			if atEOF {
				// Return the string.
				return len(data), data, nil
			}
		}
		return 0, nil, nil
	})

	var tok string
	var isNamed bool
	for scanner.Scan() {
		tok = scanner.Text()
		switch tok {
		case "[":
			if !scanner.Scan() {
				return nil, fmt.Errorf("unexpected end of JSON path")
			}
			tok = scanner.Text()
			if len(tok) < 2 || tok[0] != '\'' || tok[len(tok)-1] != '\'' {
				return nil, fmt.Errorf("expected single quoted string but got %s", tok)
			}
			unescaped, err := unescapeSingleQuote(tok[1 : len(tok)-1])
			if err != nil {
				return nil, fmt.Errorf("invalid string literal: %v", err)
			}

			if schema.Properties != nil {
				isNamed = true
			} else if schema.AdditionalProperties != nil {
				isNamed = false
			} else {
				return nil, fmt.Errorf("does not refer to a valid field")
			}
			if err := appendToPath(unescaped, isNamed); err != nil {
				return nil, err
			}
			if !scanner.Scan() {
				return nil, fmt.Errorf("unexpected end of JSON path")
			}
			tok = scanner.Text()
			if tok != "]" {
				return nil, fmt.Errorf("expected ] but got %s", tok)
			}
		case ".":
			if !scanner.Scan() {
				return nil, fmt.Errorf("unexpected end of JSON path")
			}
			tok = scanner.Text()
			if schema.Properties != nil {
				isNamed = true
			} else if schema.AdditionalProperties != nil {
				isNamed = false
			} else {
				return nil, fmt.Errorf("does not refer to a valid field")
			}
			if err := appendToPath(tok, isNamed); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("expected [ or . but got: %s", tok)
		}
	}
	return validFieldPath, nil
}

func fieldErrorForReason(fldPath *field.Path, value interface{}, detail string, reason *apiextensions.FieldValueErrorReason) *field.Error {
	if reason == nil {
		return field.Invalid(fldPath, value, detail)
	}
	switch *reason {
	case apiextensions.FieldValueForbidden:
		return field.Forbidden(fldPath, detail)
	case apiextensions.FieldValueRequired:
		return field.Required(fldPath, detail)
	case apiextensions.FieldValueDuplicate:
		return field.Duplicate(fldPath, value)
	default:
		return field.Invalid(fldPath, value, detail)
	}
}

// evalMessageExpression evaluates the given message expression and returns the evaluated string form and the remaining budget, or an error if one
// occurred during evaluation.
func evalMessageExpression(ctx context.Context, expr celgo.Program, exprSrc string, activation interpreter.Activation, remainingBudget int64) (string, int64, *cel.Error) {
	evalResult, evalDetails, err := expr.ContextEval(ctx, activation)
	if evalDetails == nil {
		return "", -1, &cel.Error{
			Type:   cel.ErrorTypeInternal,
			Detail: fmt.Sprintf("runtime cost could not be calculated for messageExpression: %q", exprSrc),
		}
	}
	rtCost := evalDetails.ActualCost()
	if rtCost == nil {
		return "", -1, &cel.Error{
			Type:   cel.ErrorTypeInternal,
			Detail: fmt.Sprintf("runtime cost could not be calculated for messageExpression: %q", exprSrc),
		}
	} else if *rtCost > math.MaxInt64 || int64(*rtCost) > remainingBudget {
		return "", -1, &cel.Error{
			Type:   cel.ErrorTypeInvalid,
			Detail: "messageExpression evaluation failed due to running out of cost budget, no further validation rules will be run",
		}
	}
	if err != nil {
		if strings.HasPrefix(err.Error(), "operation cancelled: actual cost limit exceeded") {
			return "", -1, &cel.Error{
				Type:   cel.ErrorTypeInvalid,
				Detail: fmt.Sprintf("no further validation rules will be run due to call cost exceeds limit for messageExpression: %q", exprSrc),
			}
		}
		return "", remainingBudget - int64(*rtCost), &cel.Error{
			Detail: fmt.Sprintf("messageExpression evaluation failed due to: %v", err.Error()),
		}
	}
	messageStr, ok := evalResult.Value().(string)
	if !ok {
		return "", remainingBudget - int64(*rtCost), &cel.Error{
			Detail: "messageExpression failed to convert to string",
		}
	}
	trimmedMsgStr := strings.TrimSpace(messageStr)
	if len(trimmedMsgStr) > celconfig.MaxEvaluatedMessageExpressionSizeBytes {
		return "", remainingBudget - int64(*rtCost), &cel.Error{
			Detail: fmt.Sprintf("messageExpression beyond allowable length of %d", celconfig.MaxEvaluatedMessageExpressionSizeBytes),
		}
	} else if hasNewlines(trimmedMsgStr) {
		return "", remainingBudget - int64(*rtCost), &cel.Error{
			Detail: "messageExpression should not contain line breaks",
		}
	} else if len(trimmedMsgStr) == 0 {
		return "", remainingBudget - int64(*rtCost), &cel.Error{
			Detail: "messageExpression should evaluate to a non-empty string",
		}
	}
	return trimmedMsgStr, remainingBudget - int64(*rtCost), nil
}

var newlineMatcher = regexp.MustCompile(`[\n]+`)

func hasNewlines(s string) bool {
	return newlineMatcher.MatchString(s)
}

func ruleMessageOrDefault(rule apiextensions.ValidationRule) string {
	if len(rule.Message) == 0 {
		return fmt.Sprintf("failed rule: %s", ruleErrorString(rule))
	} else {
		return strings.TrimSpace(rule.Message)
	}
}

func ruleErrorString(rule apiextensions.ValidationRule) string {
	if len(rule.Message) > 0 {
		return strings.TrimSpace(rule.Message)
	}
	return strings.TrimSpace(rule.Rule)
}

type validationActivation struct {
	self, oldSelf ref.Val
	hasOldSelf    bool
}

func validationActivationWithOldSelf(sts *schema.Structural, obj, oldObj interface{}) interpreter.Activation {
	va := &validationActivation{
		self: UnstructuredToVal(obj, sts),
	}
	if oldObj != nil {
		va.oldSelf = UnstructuredToVal(oldObj, sts) // +k8s:verify-mutation:reason=clone
		va.hasOldSelf = true                        // +k8s:verify-mutation:reason=clone
	}
	return va
}

func validationActivationWithoutOldSelf(sts *schema.Structural, obj, _ interface{}) interpreter.Activation {
	return &validationActivation{
		self: UnstructuredToVal(obj, sts),
	}
}

func (a *validationActivation) ResolveName(name string) (interface{}, bool) {
	switch name {
	case ScopedVarName:
		return a.self, true
	case OldScopedVarName:
		return a.oldSelf, a.hasOldSelf
	default:
		return nil, false
	}
}

func (a *validationActivation) Parent() interpreter.Activation {
	return nil
}

func (s *Validator) validateMap(ctx context.Context, fldPath *field.Path, sts *schema.Structural, obj, oldObj map[string]interface{}, costBudget int64) (errs field.ErrorList, remainingBudget int64) {
	remainingBudget = costBudget
	if remainingBudget < 0 {
		return errs, remainingBudget
	}
	if s == nil || obj == nil {
		return nil, remainingBudget
	}

	correlatable := MapIsCorrelatable(sts.XMapType)

	if s.AdditionalProperties != nil && sts.AdditionalProperties != nil && sts.AdditionalProperties.Structural != nil {
		for k, v := range obj {
			var oldV interface{}
			if correlatable {
				oldV = oldObj[k] // +k8s:verify-mutation:reason=clone
			}

			var err field.ErrorList
			err, remainingBudget = s.AdditionalProperties.Validate(ctx, fldPath.Key(k), sts.AdditionalProperties.Structural, v, oldV, remainingBudget)
			errs = append(errs, err...)
			if remainingBudget < 0 {
				return errs, remainingBudget
			}
		}
	}
	if s.Properties != nil && sts.Properties != nil {
		for k, v := range obj {
			stsProp, stsOk := sts.Properties[k]
			sub, ok := s.Properties[k]
			if ok && stsOk {
				var oldV interface{}
				if correlatable {
					oldV = oldObj[k] // +k8s:verify-mutation:reason=clone
				}

				var err field.ErrorList
				err, remainingBudget = sub.Validate(ctx, fldPath.Child(k), &stsProp, v, oldV, remainingBudget)
				errs = append(errs, err...)
				if remainingBudget < 0 {
					return errs, remainingBudget
				}
			}
		}
	}

	return errs, remainingBudget
}

func (s *Validator) validateArray(ctx context.Context, fldPath *field.Path, sts *schema.Structural, obj, oldObj []interface{}, costBudget int64) (errs field.ErrorList, remainingBudget int64) {
	remainingBudget = costBudget
	if remainingBudget < 0 {
		return errs, remainingBudget
	}

	if s.Items != nil && sts.Items != nil {
		// only map-type lists support self-oldSelf correlation for cel rules. if this isn't a
		// map-type list, then makeMapList returns an implementation that always returns nil
		correlatableOldItems := makeMapList(sts, oldObj)
		for i := range obj {
			var err field.ErrorList
			err, remainingBudget = s.Items.Validate(ctx, fldPath.Index(i), sts.Items, obj[i], correlatableOldItems.Get(obj[i]), remainingBudget)
			errs = append(errs, err...)
			if remainingBudget < 0 {
				return errs, remainingBudget
			}
		}
	}

	return errs, remainingBudget
}

// MapIsCorrelatable returns true if the mapType can be used to correlate the data elements of a map after an update
// with the data elements of the map from before the updated.
func MapIsCorrelatable(mapType *string) bool {
	// if a third map type is introduced, assume it's not correlatable. granular is the default if unspecified.
	return mapType == nil || *mapType == "granular" || *mapType == "atomic"
}

func hasXValidations(s *schema.Structural) bool {
	if s == nil {
		return false
	}
	if len(s.XValidations) > 0 {
		return true
	}
	if hasXValidations(s.Items) {
		return true
	}
	if s.AdditionalProperties != nil && hasXValidations(s.AdditionalProperties.Structural) {
		return true
	}
	if s.Properties != nil {
		for _, prop := range s.Properties {
			if hasXValidations(&prop) {
				return true
			}
		}
	}
	return false
}
