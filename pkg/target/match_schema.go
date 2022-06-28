package target

import "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"

// This pattern is meant to match:
//
//   REGULAR NAMESPACES
//   - These are defined by this pattern: [a-z0-9]([-a-z0-9]*[a-z0-9])?
//   - You'll see that this is the first two-thirds or so of the pattern below
//
//   PREFIX OR SUFFIX BASED WILDCARDS
//   - A typical namespace must end in an alphanumeric character.  A prefixed wildcard
//     can end in "*" (like `kube*`) or "-*" (like `kube-*`), and a suffixed wildcard
//     can start with "*" (like `*system`) or "*-" (like `*-system`).
//   - To implement this, we add either (\*|\*-)? as a prefix or (\*|-\*)? as a suffix.
//     Using both prefixed wildcards and suffixed wildcards at once is not supported.  Therefore,
//     this _does not_ allow the value to start _and_ end in a wildcard (like `*-*`).
//   - Crucially, this _does not_ allow the value to start or end in a dash (like `-system` or `kube-`).
//     That is not a valid namespace and not a wildcard, so it's disallowed.
//
//   Notably, this disallows other uses of the "*" character like:
//   - *
//   - k*-system
//
// See the following regexr to test this regex: https://regexr.com/6dgdj
const wildcardNSPattern = `^(\*|\*-)?[a-z0-9]([-a-z0-9]*[a-z0-9])?(\*|-\*)?$`

func matchSchema() apiextensions.JSONSchemaProps {
	// Define some repeatedly used sections
	wildcardNSList := apiextensions.JSONSchemaProps{
		Type: "array",
		Items: &apiextensions.JSONSchemaPropsOrArray{
			Schema: &apiextensions.JSONSchemaProps{Type: "string", Pattern: wildcardNSPattern},
		},
	}

	nullableStringList := apiextensions.JSONSchemaProps{
		Type: "array",
		Items: &apiextensions.JSONSchemaPropsOrArray{
			Schema: &apiextensions.JSONSchemaProps{Type: "string", Nullable: true},
		},
	}

	trueBool := true
	labelSelectorSchema := apiextensions.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensions.JSONSchemaProps{
			"matchLabels": {
				Type:        "object",
				Description: "A mapping of label keys to sets of allowed label values for those keys.  A selected resource will match all of these expressions.",
				AdditionalProperties: &apiextensions.JSONSchemaPropsOrBool{
					Allows: true,
					Schema: &apiextensions.JSONSchemaProps{Type: "string"},
				},
				XPreserveUnknownFields: &trueBool,
			},
			"matchExpressions": {
				Type:        "array",
				Description: "a list of label selection expressions. A selected resource will match all of these expressions.",
				Items: &apiextensions.JSONSchemaPropsOrArray{
					Schema: &apiextensions.JSONSchemaProps{
						Type:        "object",
						Description: "a selector that specifies a label key, a set of label values, an operator that defines the relationship between the two that will match the selector.",
						Properties: map[string]apiextensions.JSONSchemaProps{
							"key": {
								Description: "the label key that the selector applies to.",
								Type:        "string",
							},
							"operator": {
								Type:        "string",
								Description: "the relationship between the label and value set that defines a matching selection.",
								Enum: []apiextensions.JSON{
									"In",
									"NotIn",
									"Exists",
									"DoesNotExist",
								},
							},
							"values": {
								Type:        "array",
								Description: "a set of label values.",
								Items: &apiextensions.JSONSchemaPropsOrArray{
									Schema: &apiextensions.JSONSchemaProps{Type: "string"},
								},
							},
						},
					},
				},
			},
		},
	}

	// Make sure to copy description changes into pkg/mutation/match/match.go's `Match` struct.
	return apiextensions.JSONSchemaProps{
		Type: "object",
		Properties: map[string]apiextensions.JSONSchemaProps{
			"kinds": {
				Type: "array",
				Items: &apiextensions.JSONSchemaPropsOrArray{
					Schema: &apiextensions.JSONSchemaProps{
						Type:        "object",
						Description: "The Group and Kind of objects that should be matched.  If multiple groups/kinds combinations are specified, an incoming resource need only match one to be in scope.",
						Properties: map[string]apiextensions.JSONSchemaProps{
							"apiGroups": nullableStringList,
							"kinds":     nullableStringList,
						},
					},
				},
			},
			"namespaces":         *propsWithDescription(&wildcardNSList, "`namespaces` is a list of namespace names. If defined, a constraint only applies to resources in a listed namespace.  Namespaces also supports a prefix-based glob.  For example, `namespaces: [kube-*]` matches both `kube-system` and `kube-public`."),
			"excludedNamespaces": *propsWithDescription(&wildcardNSList, "`excludedNamespaces` is a list of namespace names. If defined, a constraint only applies to resources not in a listed namespace. ExcludedNamespaces also supports a prefix-based glob.  For example, `excludedNamespaces: [kube-*]` matches both `kube-system` and `kube-public`."),
			"labelSelector":      *propsWithDescription(&labelSelectorSchema, "`labelSelector` is the combination of two optional fields: `matchLabels` and `matchExpressions`.  These two fields provide different methods of selecting or excluding k8s objects based on the label keys and values included in object metadata.  All selection expressions from both sections are ANDed to determine if an object meets the cumulative requirements of the selector."),
			"namespaceSelector":  *propsWithDescription(&labelSelectorSchema, "`namespaceSelector` is a label selector against an object's containing namespace or the object itself, if the object is a namespace."),
			"scope": {
				Type:        "string",
				Description: "`scope` determines if cluster-scoped and/or namespaced-scoped resources are matched.  Accepts `*`, `Cluster`, or `Namespaced`. (defaults to `*`)",
				Enum: []apiextensions.JSON{
					"*",
					"Cluster",
					"Namespaced",
				},
			},
			"name": {
				Type:        "string",
				Description: "`name` is the name of an object.  If defined, it matches against objects with the specified name.  Name also supports a prefix-based glob.  For example, `name: pod-*` matches both `pod-a` and `pod-b`.",
				Pattern:     wildcardNSPattern,
			},
		},
	}
}
