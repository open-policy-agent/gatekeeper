package crds

import "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"

type MatchSchemaProvider interface {
	// MatchSchema returns the JSON Schema for the `match` field of a constraint
	MatchSchema() apiextensions.JSONSchemaProps
}
