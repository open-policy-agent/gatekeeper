package validate

import (
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func Validate(objs []*unstructured.Unstructured) (*types.Responses, error) {
	return nil, nil
}
