package client

import (
	"encoding/json"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"text/template"

	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

var _ TargetHandler = &handler{}

type handler struct{}

func (h *handler) GetName() string {
	return "test.target"
}

var libTempl = template.Must(template.New("library").Parse(`
package foo

matching_constraints[constraint] {
	constraint = {{.ConstraintsRoot}}[input.review.ForConstraint][_]
}

matching_reviews_and_constraints[[review, constraint]] {
	matching_constraints[constraint] with input as {"review": review}
	review = {{.DataRoot}}[_]
}
`))

func (h *handler) Library() *template.Template {
	return libTempl
}

func (h *handler) ProcessData(obj interface{}) (bool, string, interface{}, error) {
	switch data := obj.(type) {
	case targetData:
		return true, data.Name, &data, nil
	case *targetData:
		return true, data.Name, data, nil
	}

	return false, "", nil, nil
}

func (h *handler) HandleReview(obj interface{}) (bool, interface{}, error) {
	handled, _, review, err := h.ProcessData(obj)
	return handled, review, err
}

func (h *handler) HandleViolation(result *types.Result) error {
	res, err := json.Marshal(result.Review)
	if err != nil {
		return err
	}
	d := &targetData{}
	if err := json.Unmarshal(res, d); err != nil {
		return err
	}
	result.Resource = d
	return nil
}

func (h *handler) MatchSchema() apiextensionsv1beta1.JSONSchemaProps {
	return apiextensionsv1beta1.JSONSchemaProps{
		Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
			"label": apiextensionsv1beta1.JSONSchemaProps{Type: "string"},
		},
	}
}

func (h *handler) ValidateConstraint(u *unstructured.Unstructured) error {
	return nil
}

type targetData struct {
	Name          string
	ForConstraint string
	vals          map[string]interface{}
}
