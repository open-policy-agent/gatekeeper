package gator

import (
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/k8scel"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/llm"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
)

func NewOPAClient(includeTrace bool, k8sCEL bool, useLLM bool) (Client, error) {
	args := []constraintclient.Opt{constraintclient.Targets(&target.K8sValidationTarget{})}

	if k8sCEL {
		k8sDriver, err := k8scel.New()
		if err != nil {
			return nil, err
		}
		args = append(args, constraintclient.Driver(k8sDriver))
	}

	if useLLM {
		llmDriver, err := llm.New()
		if err != nil {
			return nil, err
		}
		args = append(args, constraintclient.Driver(llmDriver))
	}

	driver, err := rego.New(rego.Tracing(includeTrace))
	if err != nil {
		return nil, err
	}

	args = append(args, constraintclient.Driver(driver))

	c, err := constraintclient.NewClient(args...)
	if err != nil {
		return nil, err
	}

	return c, nil
}
