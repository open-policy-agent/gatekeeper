package schema

import (
	"errors"

	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// Name is the name of the driver.
	Name = "LLM"
)

var (
	ErrBadType = errors.New("could not recognize the type")
)

type Source struct {
	Prompt string `json:"prompt,omitempty"`
}

func (in *Source) GetPrompt() (string, error) {
	if in == nil {
		return "", nil
	}
	return in.Prompt, nil
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

	return out, nil
}

func GetSourceFromTemplate(ct *templates.ConstraintTemplate) (*Source, error) {
	if len(ct.Spec.Targets) != 1 {
		return nil, errors.New("wrong number of targets defined, only 1 target allowed")
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
		return nil, errors.New("LLM code not defined")
	}
	return source, nil
}
