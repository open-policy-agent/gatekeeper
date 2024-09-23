package transform

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel/schema"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/policy/validating"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/matchconditions"
)

const (
	matchKinds = `
	!has(params.spec) ? true: (
		!has(params.spec.match) ? true: (
			!has(params.spec.match.kinds) ? true : (
				params.spec.match.kinds.exists(groupskinds,
					(!has(groupskinds.kinds) || size(groupskinds.kinds) == 0 || "*" in groupskinds.kinds || request.kind.kind in groupskinds.kinds) &&
					(!has(groupskinds.apiGroups) || size(groupskinds.apiGroups) == 0 || "*" in groupskinds.apiGroups || request.kind.group in groupskinds.apiGroups)
				)
			)
		)
	)
	`

	// Note that switching the glob to a regex is valid because of how Gatekeeper validates the wildcard matcher
	// (with this regex: "+kubebuilder:validation:Pattern=`^(\*|\*-)?[a-z0-9]([-:a-z0-9]*[a-z0-9])?(\*|-\*)?$`").
	matchNameGlob = `
	!has(params.spec) ? true: (
		!has(params.spec.match) ? true: (
			!has(params.spec.match.name) ? true : (
				[object, oldObject].exists(obj,
					obj != null && (
						(has(obj.metadata.generateName) && obj.metadata.generateName != "" && params.spec.match.name.endsWith("*") && string(obj.metadata.generateName).matches("^" + string(params.spec.match.name).replace("*", ".*") + "$")) ||
						(has(obj.metadata.name) && string(obj.metadata.name).matches("^" + string(params.spec.match.name).replace("*", ".*") + "$"))
					)
				)
			)
		)
	)
	`

	// Note that switching the glob to a regex is valid because of how Gatekeeper validates the wildcard matcher
	// (with this regex: "+kubebuilder:validation:Pattern=`^(\*|\*-)?[a-z0-9]([-:a-z0-9]*[a-z0-9])?(\*|-\*)?$`").
	// TODO: consider using the `namespaceObject` field provided by ValidatingAdmissionPolicy.
	matchNamespacesGlob = `
	!has(params.spec) ? true: (
		!has(params.spec.match) ? true: (
			!has(params.spec.match.namespaces) ? true : (
				[object, oldObject].exists(obj,
					obj != null && (
						// cluster-scoped objects always match
						!has(obj.metadata.namespace) || obj.metadata.namespace == "" ? true : (
							params.spec.match.namespaces.exists(nsMatcher,
								(string(obj.metadata.namespace).matches("^" + string(nsMatcher).replace("*", ".*") + "$"))
							)
						)
					)
				)
			)
		)
	)
	`

	// Note that switching the glob to a regex is valid because of how Gatekeeper validates the wildcard matcher
	// (with this regex: "+kubebuilder:validation:Pattern=`^(\*|\*-)?[a-z0-9]([-:a-z0-9]*[a-z0-9])?(\*|-\*)?$`").
	// TODO: consider using the `namespaceObject` field provided by ValidatingAdmissionPolicy.
	matchExcludedNamespacesGlob = `
	!has(params.spec) ? true: (
		!has(params.spec.match) ? true: (
			!has(params.spec.match.excludedNamespaces) ? true : (
				[object, oldObject].exists(obj,
					obj != null && (
						// cluster-scoped objects always match
						!has(obj.metadata.namespace) || obj.metadata.namespace == "" ? true : (
							!params.spec.match.excludedNamespaces.exists(nsMatcher,
								(string(obj.metadata.namespace).matches("^" + string(nsMatcher).replace("*", ".*") + "$"))
							)
						)
					)
				)
			)
		)
	)
	`
)

const StrictCost = true

func MatchExcludedNamespacesGlobV1Beta1() admissionregistrationv1beta1.MatchCondition {
	return admissionregistrationv1beta1.MatchCondition{
		Name:       "gatekeeper_internal_match_excluded_namespaces",
		Expression: matchExcludedNamespacesGlob,
	}
}

func MatchExcludedNamespacesGlobCEL() []cel.ExpressionAccessor {
	mc := MatchExcludedNamespacesGlobV1Beta1()
	return []cel.ExpressionAccessor{
		&matchconditions.MatchCondition{
			Name:       mc.Name,
			Expression: mc.Expression,
		},
	}
}

func MatchNamespacesGlobV1Beta1() admissionregistrationv1beta1.MatchCondition {
	return admissionregistrationv1beta1.MatchCondition{
		Name:       "gatekeeper_internal_match_namespaces",
		Expression: matchNamespacesGlob,
	}
}

func MatchNamespacesGlobCEL() []cel.ExpressionAccessor {
	mc := MatchNamespacesGlobV1Beta1()
	return []cel.ExpressionAccessor{
		&matchconditions.MatchCondition{
			Name:       mc.Name,
			Expression: mc.Expression,
		},
	}
}

func MatchNameGlobV1Beta1() admissionregistrationv1beta1.MatchCondition {
	return admissionregistrationv1beta1.MatchCondition{
		Name:       "gatekeeper_internal_match_name",
		Expression: matchNameGlob,
	}
}

func MatchNameGlobCEL() []cel.ExpressionAccessor {
	mc := MatchNameGlobV1Beta1()
	return []cel.ExpressionAccessor{
		&matchconditions.MatchCondition{
			Name:       mc.Name,
			Expression: mc.Expression,
		},
	}
}

func MatchKindsV1Beta1() admissionregistrationv1beta1.MatchCondition {
	return admissionregistrationv1beta1.MatchCondition{
		Name:       "gatekeeper_internal_match_kinds",
		Expression: matchKinds,
	}
}

func MatchKindsCEL() []cel.ExpressionAccessor {
	mc := MatchKindsV1Beta1()
	return []cel.ExpressionAccessor{
		&matchconditions.MatchCondition{
			Name:       mc.Name,
			Expression: mc.Expression,
		},
	}
}

func BindParamsV1Beta1() admissionregistrationv1beta1.Variable {
	return admissionregistrationv1beta1.Variable{
		Name:       schema.ParamsName,
		Expression: "!has(params.spec) ? null : !has(params.spec.parameters) ? null: params.spec.parameters",
	}
}

func BindParamsCEL() cel.NamedExpressionAccessor {
	v := BindParamsV1Beta1()
	return &validating.Variable{
		Name:       v.Name,
		Expression: v.Expression,
	}
}

func BindObjectV1Beta1() admissionregistrationv1beta1.Variable {
	return admissionregistrationv1beta1.Variable{
		Name:       schema.ObjectName,
		Expression: `has(request.operation) && request.operation == "DELETE" && object == null ? oldObject : object`,
	}
}

func BindObjectCEL() cel.NamedExpressionAccessor {
	v := BindObjectV1Beta1()
	return &validating.Variable{
		Name:       v.Name,
		Expression: v.Expression,
	}
}

func AllMatchersV1Beta1() []admissionregistrationv1beta1.MatchCondition {
	return []admissionregistrationv1beta1.MatchCondition{
		MatchExcludedNamespacesGlobV1Beta1(),
		MatchNamespacesGlobV1Beta1(),
		MatchNameGlobV1Beta1(),
		MatchKindsV1Beta1(),
	}
}

func AllVariablesCEL() []cel.NamedExpressionAccessor {
	vars := AllVariablesV1Beta1()
	xform := make([]cel.NamedExpressionAccessor, len(vars))
	for i := range vars {
		xform[i] = &validating.Variable{
			Name:       vars[i].Name,
			Expression: vars[i].Expression,
		}
	}
	return xform
}

func AllVariablesV1Beta1() []admissionregistrationv1beta1.Variable {
	return []admissionregistrationv1beta1.Variable{
		BindObjectV1Beta1(),
		BindParamsV1Beta1(),
	}
}
