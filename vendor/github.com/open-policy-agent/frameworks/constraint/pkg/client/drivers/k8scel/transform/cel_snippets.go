package transform

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel/schema"
	admissionregistrationv1alpha1 "k8s.io/api/admissionregistration/v1alpha1"
	"k8s.io/apiserver/pkg/admission/plugin/cel"
	"k8s.io/apiserver/pkg/admission/plugin/validatingadmissionpolicy"
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
				(has(object.metadata.generateName) && object.metadata.generateName != "" && params.spec.match.name.endsWith("*") && string(object.metadata.generateName).matches("^" + string(params.spec.match.name).replace("*", ".*") + "$")) ||
				(has(object.metadata.name) && string(object.metadata.name).matches("^" + string(params.spec.match.name).replace("*", ".*") + "$"))
			)
		)
	)
	`

	// Note that switching the glob to a regex is valid because of how Gatekeeper validates the wildcard matcher
	// (with this regex: "+kubebuilder:validation:Pattern=`^(\*|\*-)?[a-z0-9]([-:a-z0-9]*[a-z0-9])?(\*|-\*)?$`").
	matchNamespacesGlob = `
	!has(params.spec) ? true: (
		!has(params.spec.match) ? true: (
			!has(params.spec.match.namespaces) ? true : (
				// cluster-scoped objects always match
				!has(object.metadata.namespace) || object.metadata.namespace == "" ? true : (
					params.spec.match.namespaces.exists(nsMatcher,
						(string(object.metadata.namespace).matches("^" + string(nsMatcher).replace("*", ".*") + "$"))
					)
				)
			)
		)
	)
	`

	// Note that switching the glob to a regex is valid because of how Gatekeeper validates the wildcard matcher
	// (with this regex: "+kubebuilder:validation:Pattern=`^(\*|\*-)?[a-z0-9]([-:a-z0-9]*[a-z0-9])?(\*|-\*)?$`").
	matchExcludedNamespacesGlob = `
	!has(params.spec) ? true: (
		!has(params.spec.match) ? true: (
			!has(params.spec.match.excludedNamespaces) ? true : (
					// cluster-scoped objects always match
					!has(object.metadata.namespace) || object.metadata.namespace == "" ? true : (
						!params.spec.match.excludedNamespaces.exists(nsMatcher,
							(string(object.metadata.namespace).matches("^" + string(nsMatcher).replace("*", ".*") + "$"))
						)
					)
			)
		)
	)
	`
)

func MatchExcludedNamespacesGlobV1Alpha1() admissionregistrationv1alpha1.MatchCondition {
	return admissionregistrationv1alpha1.MatchCondition{
		Name:       "gatekeeper_internal_match_excluded_namespaces",
		Expression: matchExcludedNamespacesGlob,
	}
}

func MatchExcludedNamespacesGlobCEL() []cel.ExpressionAccessor {
	mc := MatchExcludedNamespacesGlobV1Alpha1()
	return []cel.ExpressionAccessor{
		&matchconditions.MatchCondition{
			Name:       mc.Name,
			Expression: mc.Expression,
		},
	}
}

func MatchNamespacesGlobV1Alpha1() admissionregistrationv1alpha1.MatchCondition {
	return admissionregistrationv1alpha1.MatchCondition{
		Name:       "gatekeeper_internal_match_namespaces",
		Expression: matchNamespacesGlob,
	}
}

func MatchNamespacesGlobCEL() []cel.ExpressionAccessor {
	mc := MatchNamespacesGlobV1Alpha1()
	return []cel.ExpressionAccessor{
		&matchconditions.MatchCondition{
			Name:       mc.Name,
			Expression: mc.Expression,
		},
	}
}

func MatchNameGlobV1Alpha1() admissionregistrationv1alpha1.MatchCondition {
	return admissionregistrationv1alpha1.MatchCondition{
		Name:       "gatekeeper_internal_match_name",
		Expression: matchNameGlob,
	}
}

func MatchNameGlobCEL() []cel.ExpressionAccessor {
	mc := MatchNameGlobV1Alpha1()
	return []cel.ExpressionAccessor{
		&matchconditions.MatchCondition{
			Name:       mc.Name,
			Expression: mc.Expression,
		},
	}
}

func MatchKindsV1Alpha1() admissionregistrationv1alpha1.MatchCondition {
	return admissionregistrationv1alpha1.MatchCondition{
		Name:       "gatekeeper_internal_match_kinds",
		Expression: matchKinds,
	}
}

func MatchKindsCEL() []cel.ExpressionAccessor {
	mc := MatchKindsV1Alpha1()
	return []cel.ExpressionAccessor{
		&matchconditions.MatchCondition{
			Name:       mc.Name,
			Expression: mc.Expression,
		},
	}
}

func BindParamsV1Alpha1() admissionregistrationv1alpha1.Variable {
	return admissionregistrationv1alpha1.Variable{
		Name:       schema.ParamsName,
		Expression: "params.spec.parameters",
	}
}

func BindParamsCEL() []cel.NamedExpressionAccessor {
	v := BindParamsV1Alpha1()
	return []cel.NamedExpressionAccessor{
		&validatingadmissionpolicy.Variable{
			Name:       v.Name,
			Expression: v.Expression,
		},
	}
}

func AllMatchersV1Alpha1() []admissionregistrationv1alpha1.MatchCondition {
	return []admissionregistrationv1alpha1.MatchCondition{
		MatchExcludedNamespacesGlobV1Alpha1(),
		MatchNamespacesGlobV1Alpha1(),
		MatchNameGlobV1Alpha1(),
		MatchKindsV1Alpha1(),
	}
}

func AllVariablesCEL() []cel.NamedExpressionAccessor {
	return BindParamsCEL()
}

func AllVariablesV1Alpha1() []admissionregistrationv1alpha1.Variable {
	return []admissionregistrationv1alpha1.Variable{
		BindParamsV1Alpha1(),
	}
}
