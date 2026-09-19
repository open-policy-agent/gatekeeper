package transform

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	celgo "github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"
	celparser "github.com/google/cel-go/parser/gen"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel/schema"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apiserver/pkg/cel/environment"
)

// ErrDirectParameterReference indicates CEL that requires the VAP top-level params variable.
var ErrDirectParameterReference = errors.New("direct params references are not supported with per-constraint VAP generation; use variables.params")

const (
	celTrueLiteral               = "true"
	matchExcludedNamespacesField = "excludedNamespaces"
)

func constraintParametersExpression(constraint *unstructured.Unstructured) (string, error) {
	parameters, found, err := unstructured.NestedFieldNoCopy(constraint.Object, "spec", "parameters")
	if err != nil {
		return "", err
	}
	if !found || parameters == nil {
		return "dyn(null)", nil
	}
	return compactParametersExpression(parameters)
}

func constraintMatchInputExpression(constraint *unstructured.Unstructured) (string, error) {
	match, found, err := unstructured.NestedMap(constraint.Object, "spec", "match")
	if err != nil {
		return "", err
	}
	if !found {
		return jsonValueToCEL(map[string]interface{}{"spec": map[string]interface{}{}})
	}
	return jsonValueToCEL(map[string]interface{}{
		"spec": map[string]interface{}{
			"match": match,
		},
	})
}

func jsonValueToCEL(value interface{}) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(typed), nil
	case string:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	case int:
		return strconv.FormatInt(int64(typed), 10), nil
	case int8:
		return strconv.FormatInt(int64(typed), 10), nil
	case int16:
		return strconv.FormatInt(int64(typed), 10), nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case uint:
		return strconv.FormatUint(uint64(typed), 10) + "u", nil
	case uint8:
		return strconv.FormatUint(uint64(typed), 10) + "u", nil
	case uint16:
		return strconv.FormatUint(uint64(typed), 10) + "u", nil
	case uint32:
		return strconv.FormatUint(uint64(typed), 10) + "u", nil
	case uint64:
		return strconv.FormatUint(typed, 10) + "u", nil
	case float32:
		return floatToCEL(float64(typed), 32)
	case float64:
		return floatToCEL(typed, 64)
	case json.Number:
		if strings.ContainsAny(string(typed), ".eE") {
			value, err := typed.Float64()
			if err != nil {
				return "", err
			}
			return floatToCEL(value, 64)
		}
		if _, err := typed.Int64(); err != nil {
			return "", err
		}
		return string(typed), nil
	case []interface{}:
		values := make([]string, 0, len(typed))
		for index, item := range typed {
			expression, err := jsonValueToCEL(item)
			if err != nil {
				return "", fmt.Errorf("list item %d: %w", index, err)
			}
			values = append(values, "dyn("+expression+")")
		}
		return "[" + strings.Join(values, ", ") + "]", nil
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		entries := make([]string, 0, len(keys))
		for _, key := range keys {
			expression, err := jsonValueToCEL(typed[key])
			if err != nil {
				return "", fmt.Errorf("property %q: %w", key, err)
			}
			encodedKey, err := json.Marshal(key)
			if err != nil {
				return "", err
			}
			entries = append(entries, string(encodedKey)+": dyn("+expression+")")
		}
		return "{" + strings.Join(entries, ", ") + "}", nil
	default:
		return "", fmt.Errorf("unsupported JSON value type %T", value)
	}
}

func floatToCEL(value float64, bitSize int) (string, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "", fmt.Errorf("non-finite float %v is not valid JSON", value)
	}
	expression := strconv.FormatFloat(value, 'g', -1, bitSize)
	if !strings.ContainsAny(expression, ".eE") {
		expression += ".0"
	}
	return expression, nil
}

func parseInlineExpression(expression string) (*celgo.Ast, error) {
	env := environment.MustBaseEnvSet(environment.DefaultCompatibilityVersion()).StoredExpressionsEnv()
	parsed, issues := env.Parse(expression)
	if issues.Err() != nil {
		return nil, issues.Err()
	}
	return parsed, nil
}

func visitFreeIdentifiers(root celast.Expr, identifier string, visitor func(celast.Expr)) {
	var walk func(celast.Expr, bool)
	walk = func(expression celast.Expr, shadowed bool) {
		switch expression.Kind() {
		case celast.IdentKind:
			if expression.AsIdent() == "."+identifier || (!shadowed && expression.AsIdent() == identifier) {
				visitor(expression)
			}
		case celast.SelectKind:
			walk(expression.AsSelect().Operand(), shadowed)
		case celast.CallKind:
			call := expression.AsCall()
			if call.IsMemberFunction() {
				walk(call.Target(), shadowed)
			}
			for _, argument := range call.Args() {
				walk(argument, shadowed)
			}
		case celast.ListKind:
			for _, element := range expression.AsList().Elements() {
				walk(element, shadowed)
			}
		case celast.MapKind:
			for _, entry := range expression.AsMap().Entries() {
				walk(entry.AsMapEntry().Key(), shadowed)
				walk(entry.AsMapEntry().Value(), shadowed)
			}
		case celast.StructKind:
			for _, field := range expression.AsStruct().Fields() {
				walk(field.AsStructField().Value(), shadowed)
			}
		case celast.ComprehensionKind:
			comprehension := expression.AsComprehension()
			walk(comprehension.IterRange(), shadowed)
			walk(comprehension.AccuInit(), shadowed)
			accumulatorShadows := shadowed || comprehension.AccuVar() == identifier
			loopShadows := accumulatorShadows || comprehension.IterVar() == identifier || comprehension.IterVar2() == identifier
			walk(comprehension.LoopCondition(), loopShadows)
			walk(comprehension.LoopStep(), loopShadows)
			walk(comprehension.Result(), accumulatorShadows)
		}
	}
	walk(root, false)
}

func expressionReferencesIdentifier(expression, identifier string) (bool, error) {
	parsed, err := parseInlineExpression(expression)
	if err != nil {
		return false, err
	}
	found := false
	visitFreeIdentifiers(parsed.NativeRep().Expr(), identifier, func(celast.Expr) { found = true })
	return found, nil
}

func replaceIdentifier(expression, identifier, replacement string) (string, error) {
	parsed, err := parseInlineExpression(expression)
	if err != nil {
		return "", err
	}
	if _, err := parseInlineExpression(replacement); err != nil {
		return "", err
	}

	type offsetRange struct {
		start           int
		stop            int
		identifierStart int
	}
	ranges := make(map[offsetRange]struct{})
	var qualifiedOffsets map[int]int
	var collectErr error
	visitFreeIdentifiers(parsed.NativeRep().Expr(), identifier, func(reference celast.Expr) {
		offset, found := parsed.NativeRep().SourceInfo().GetOffsetRange(reference.ID())
		if !found {
			collectErr = fmt.Errorf("source range not found for %q identifier", identifier)
			return
		}
		start := int(offset.Start)
		if reference.AsIdent() == "."+identifier {
			if qualifiedOffsets == nil {
				qualifiedOffsets = qualifiedIdentifierOffsets(expression)
			}
			start, found = qualifiedOffsets[start]
			if !found {
				collectErr = fmt.Errorf("qualifier not found for %q identifier", identifier)
				return
			}
		}
		ranges[offsetRange{start: start, stop: int(offset.Stop), identifierStart: int(offset.Start)}] = struct{}{}
	})
	if collectErr != nil {
		return "", collectErr
	}
	if len(ranges) == 0 {
		return expression, nil
	}
	ordered := make([]offsetRange, 0, len(ranges))
	for offset := range ranges {
		ordered = append(ordered, offset)
	}
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].start > ordered[right].start
	})
	rewritten := []rune(expression)
	for _, offset := range ordered {
		if offset.start < 0 || offset.identifierStart < offset.start || offset.stop > len(rewritten) || offset.identifierStart >= offset.stop || string(rewritten[offset.identifierStart:offset.stop]) != identifier {
			return "", fmt.Errorf("invalid source range [%d:%d] for %q identifier", offset.start, offset.stop, identifier)
		}
		rewritten = []rune(string(rewritten[:offset.start]) + "(" + replacement + ")" + string(rewritten[offset.stop:]))
	}
	return string(rewritten), nil
}

func qualifiedIdentifierOffsets(expression string) map[int]int {
	lexer := celparser.NewCELLexer(antlr.NewInputStream(expression))
	starts := make(map[int]int)
	dot := -1
	for token := lexer.NextToken(); token.GetTokenType() != antlr.TokenEOF; token = lexer.NextToken() {
		if token.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		if dot >= 0 {
			starts[token.GetStart()] = dot
		}
		dot = -1
		if token.GetTokenType() == celparser.CELLexerDOT {
			dot = token.GetStart()
		}
	}
	return starts
}

func validateNoDirectParameterReferences(source *schema.Source) error {
	expressions := make([]string, 0, len(source.MatchConditions)+len(source.Variables)+2*len(source.Validations))
	for _, condition := range source.MatchConditions {
		expressions = append(expressions, condition.Expression)
	}
	for _, variable := range source.Variables {
		expressions = append(expressions, variable.Expression)
	}
	for _, validation := range source.Validations {
		expressions = append(expressions, validation.Expression)
		if validation.MessageExpression != "" {
			expressions = append(expressions, validation.MessageExpression)
		}
	}
	for _, expression := range expressions {
		referencesParams, err := expressionReferencesIdentifier(expression, "params")
		if err != nil {
			return err
		}
		if referencesParams {
			return fmt.Errorf("%w: %s", ErrDirectParameterReference, expression)
		}
	}
	return nil
}

func specializeMatchConditions(conditions []admissionregistrationv1beta1.MatchCondition, constraint *unstructured.Unstructured) ([]admissionregistrationv1beta1.MatchCondition, error) {
	match, _, err := unstructured.NestedMap(constraint.Object, "spec", "match")
	if err != nil {
		return nil, err
	}
	constraintExpression, err := constraintMatchInputExpression(constraint)
	if err != nil {
		return nil, err
	}
	result := make([]admissionregistrationv1beta1.MatchCondition, 0, len(conditions))
	for _, condition := range conditions {
		var field string
		switch condition {
		case MatchKindsV1Beta1():
			field = "kinds"
		case MatchNameGlobV1Beta1():
			field = "name"
		case MatchNamespacesGlobV1Beta1():
			field = "namespaces"
		case MatchExcludedNamespacesGlobV1Beta1():
			field = matchExcludedNamespacesField
		}
		if _, present := match[field]; field != "" && !present {
			continue
		}
		var expression string
		var specialized bool
		switch field {
		case "kinds":
			expression, specialized = specializeKindMatch(match[field])
		case "name":
			expression, specialized = specializeNameMatch(match[field])
		case "namespaces", matchExcludedNamespacesField:
			expression, specialized = specializeNamespaceMatch(match[field], field == matchExcludedNamespacesField)
		}
		if specialized {
			condition.Expression = expression
			result = append(result, condition)
			continue
		}
		condition.Expression, err = replaceIdentifier(condition.Expression, "params", constraintExpression)
		if err != nil {
			return nil, fmt.Errorf("specialize match condition %q: %w", condition.Name, err)
		}
		result = append(result, condition)
	}
	return result, nil
}

func specializeKindMatch(value interface{}) (string, bool) {
	entries, ok := value.([]interface{})
	if !ok {
		return "", false
	}
	clauses := make([]string, 0, len(entries))
	for _, item := range entries {
		entry, ok := item.(map[string]interface{})
		if !ok {
			return "", false
		}
		kind, ok := specializeKindSet(entry, "kinds", "request.kind.kind")
		if !ok {
			return "", false
		}
		group, ok := specializeKindSet(entry, "apiGroups", "request.kind.group")
		if !ok {
			return "", false
		}
		switch {
		case kind == celTrueLiteral:
			clauses = append(clauses, group)
		case group == celTrueLiteral:
			clauses = append(clauses, kind)
		default:
			clauses = append(clauses, "("+kind+" && "+group+")")
		}
	}
	if len(clauses) == 0 {
		return "false", true
	}
	return strings.Join(clauses, " || "), true
}

func specializeKindSet(entry map[string]interface{}, field, subject string) (string, bool) {
	value, present := entry[field]
	if !present {
		return celTrueLiteral, true
	}
	items, ok := value.([]interface{})
	if !ok {
		return "", false
	}
	values := make([]string, 0, len(items))
	wildcard := false
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return "", false
		}
		wildcard = wildcard || text == "*"
		values = append(values, strconv.Quote(text))
	}
	if wildcard || len(values) == 0 {
		return celTrueLiteral, true
	}
	if len(values) == 1 {
		return subject + " == " + values[0], true
	}
	return subject + " in [" + strings.Join(values, ", ") + "]", true
}

func specializeNameMatch(value interface{}) (string, bool) {
	name, ok := value.(string)
	if !ok {
		return "", false
	}
	pattern, ok := specializedGlobPattern(name)
	if !ok {
		return "", false
	}
	predicate := "has(obj.metadata.name) && string(obj.metadata.name).matches(" + pattern + ")"
	if strings.HasSuffix(name, "*") {
		predicate = "(has(obj.metadata.generateName) && obj.metadata.generateName != \"\" && string(obj.metadata.generateName).matches(" + pattern + ")) || (" + predicate + ")"
	}
	return "[object, oldObject].exists(obj, obj != null && (" + predicate + "))", true
}

func specializeNamespaceMatch(value interface{}, excluded bool) (string, bool) {
	namespaces, ok := value.([]interface{})
	if !ok {
		return "", false
	}
	clauses := make([]string, 0, len(namespaces))
	patterns := make([]string, 0, len(namespaces))
	for _, item := range namespaces {
		namespace, ok := item.(string)
		if !ok {
			return "", false
		}
		pattern, ok := specializedGlobPattern(namespace)
		if !ok {
			return "", false
		}
		patterns = append(patterns, pattern)
		clauses = append(clauses, "string(obj.metadata.namespace).matches("+pattern+")")
	}
	predicate := "false"
	if len(clauses) > 0 {
		predicate = strings.Join(clauses, " || ")
	}
	if len(patterns) > 8 {
		predicate = "[" + strings.Join(patterns, ", ") + "].exists(pattern, string(obj.metadata.namespace).matches(pattern))"
	}
	if excluded {
		predicate = "!(" + predicate + ")"
	}
	return "[object, oldObject].exists(obj, obj != null && (!has(obj.metadata.namespace) || obj.metadata.namespace == \"\" ? true : (" + predicate + ")))", true
}

func specializedGlobPattern(glob string) (string, bool) {
	pattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(glob), `\*`, ".*") + "$"
	if _, err := regexp.Compile(pattern); err != nil {
		return "", false
	}
	return strconv.Quote(pattern), true
}
