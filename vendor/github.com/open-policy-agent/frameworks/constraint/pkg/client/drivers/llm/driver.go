package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"

	apiconstraints "github.com/open-policy-agent/frameworks/constraint/pkg/apis/constraints"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers"
	llmSchema "github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/llm/schema"
	"github.com/open-policy-agent/frameworks/constraint/pkg/core/templates"
	"github.com/open-policy-agent/frameworks/constraint/pkg/types"
	"github.com/open-policy-agent/opa/storage"
	"github.com/sethvargo/go-retry"
	flag "github.com/spf13/pflag"
	"github.com/walles/env"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	maxRetries = 10
	// need minimum of 2023-12-01-preview for JSON mode
	azureOpenAIAPIVersion = "2024-03-01-preview"
	azureOpenAIURL        = "openai.azure.com"
	systemPrompt          = "You are a policy engine for Kubernetes designed to output JSON. Input will be a policy definition, Kubernetes AdmissionRequest object, and parameters to apply to the policy if applicable. Output JSON should only have a 'decision' field with a boolean value and a 'reason' field with a string value explaining the decision, only if decision is false. Only output valid JSON."
)

var (
	openAIAPIURLv1 = "https://api.openai.com/v1"

	openAIDeploymentName = flag.String("openai-deployment-name", env.GetOr("OPENAI_DEPLOYMENT_NAME", env.String, "gpt-3.5-turbo-0301"), "The deployment name used for the model in OpenAI service.")
	openAIAPIKey         = flag.String("openai-api-key", env.GetOr("OPENAI_API_KEY", env.String, ""), "The API key for the OpenAI service. This is required.")
	openAIEndpoint       = flag.String("openai-endpoint", env.GetOr("OPENAI_ENDPOINT", env.String, openAIAPIURLv1), "The endpoint for OpenAI service. Defaults to"+openAIAPIURLv1+". Set this to Azure OpenAI Service or OpenAI compatible API endpoint, if needed.")
)

type Driver struct {
	prompts map[string]string
}

var _ drivers.Driver = &Driver{}

type Decision struct {
	Name       string
	Constraint *unstructured.Unstructured
	Decision   bool
	Reason     string
}

type ARGetter interface {
	GetAdmissionRequest() *admissionv1.AdmissionRequest
}

// Name returns the name of the driver.
func (d *Driver) Name() string {
	return llmSchema.Name
}

func (d *Driver) AddTemplate(_ context.Context, ct *templates.ConstraintTemplate) error {
	source, err := llmSchema.GetSourceFromTemplate(ct)
	if err != nil {
		return err
	}

	prompt, err := source.GetPrompt()
	if err != nil {
		return err
	}
	if prompt == "" {
		return fmt.Errorf("prompt is empty for template: %q", ct.Name)
	}

	d.prompts[ct.Name] = prompt
	return nil
}

func (d *Driver) RemoveTemplate(_ context.Context, ct *templates.ConstraintTemplate) error {
	delete(d.prompts, ct.Name)

	return nil
}

func (d *Driver) AddConstraint(_ context.Context, constraint *unstructured.Unstructured) error {
	promptName := strings.ToLower(constraint.GetKind())

	_, found := d.prompts[promptName]
	if !found {
		return fmt.Errorf("no promptName with name: %q", promptName)
	}
	return nil
}

func (d *Driver) RemoveConstraint(_ context.Context, _ *unstructured.Unstructured) error {
	return nil
}

func (d *Driver) AddData(_ context.Context, _ string, _ storage.Path, _ interface{}) error {
	return nil
}

func (d *Driver) RemoveData(_ context.Context, _ string, _ storage.Path) error {
	return nil
}

func (d *Driver) Query(ctx context.Context, _ string, constraints []*unstructured.Unstructured, review interface{}, _ ...drivers.QueryOpt) (*drivers.QueryResponse, error) {
	llmc, err := newLLMClients()
	if err != nil {
		return nil, err
	}

	arGetter, ok := review.(ARGetter)
	if !ok {
		return nil, errors.New("cannot convert review to ARGetter")
	}
	aRequest := arGetter.GetAdmissionRequest()

	var allDecisions []*Decision
	for _, constraint := range constraints {
		promptName := strings.ToLower(constraint.GetKind())
		prompt, found := d.prompts[promptName]
		if !found {
			continue
		}

		paramsStruct, _, err := unstructured.NestedFieldNoCopy(constraint.Object, "spec", "parameters")
		if err != nil {
			return nil, err
		}

		params, err := json.Marshal(paramsStruct)
		if err != nil {
			return nil, err
		}

		llmPrompt := fmt.Sprintf("policy: %s\nadmission request: %s\nparameters: %s", prompt, string(aRequest.Object.Raw), string(params))

		var resp string
		r := retry.WithMaxRetries(maxRetries, retry.NewExponential(1*time.Second))
		if err := retry.Do(ctx, r, func(ctx context.Context) error {
			resp, err = llmc.openaiGptChatCompletion(ctx, llmPrompt)
			requestErr := &openai.APIError{}
			if errors.As(err, &requestErr) {
				switch requestErr.HTTPStatusCode {
				case http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
					return retry.RetryableError(err)
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}

		var decision Decision
		err = json.Unmarshal([]byte(resp), &decision)
		if err != nil {
			return nil, err
		}

		if !decision.Decision {
			llmDecision := &Decision{
				Decision:   decision.Decision,
				Name:       constraint.GetName(),
				Constraint: constraint,
				Reason:     decision.Reason,
			}
			allDecisions = append(allDecisions, llmDecision)
		}
	}
	if len(allDecisions) == 0 {
		return nil, nil
	}

	results := make([]*types.Result, len(allDecisions))
	for i, llmDecision := range allDecisions {
		enforcementAction, found, err := unstructured.NestedString(llmDecision.Constraint.Object, "spec", "enforcementAction")
		if err != nil {
			return nil, err
		}
		if !found {
			enforcementAction = apiconstraints.EnforcementActionDeny
		}

		results[i] = &types.Result{
			Metadata: map[string]interface{}{
				"name": llmDecision.Name,
			},
			Constraint:        llmDecision.Constraint,
			Msg:               llmDecision.Reason,
			EnforcementAction: enforcementAction,
		}
	}
	return &drivers.QueryResponse{Results: results}, nil
}

func (d *Driver) Dump(_ context.Context) (string, error) {
	panic("implement me")
}

func (d *Driver) GetDescriptionForStat(_ string) (string, error) {
	panic("implement me")
}

type llmClients struct {
	openAIClient openai.Client
}

func newLLMClients() (llmClients, error) {
	var config openai.ClientConfig
	// default to OpenAI API
	config = openai.DefaultConfig(*openAIAPIKey)

	if openAIEndpoint != &openAIAPIURLv1 {
		// Azure OpenAI
		if strings.Contains(*openAIEndpoint, azureOpenAIURL) {
			config = openai.DefaultAzureConfig(*openAIAPIKey, *openAIEndpoint)
		} else {
			// OpenAI API compatible endpoint or proxy
			config.BaseURL = *openAIEndpoint
		}
		config.APIVersion = azureOpenAIAPIVersion
	}

	clients := llmClients{
		openAIClient: *openai.NewClientWithConfig(config),
	}
	return clients, nil
}

func (c *llmClients) openaiGptChatCompletion(ctx context.Context, prompt string) (string, error) {
	req := openai.ChatCompletionRequest{
		Model: *openAIDeploymentName,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: systemPrompt,
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
		N:           1, // Number of completions to generate
		Temperature: 0, // 0 is more deterministic
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
	}

	resp, err := c.openAIClient.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) != 1 {
		return "", fmt.Errorf("expected choices to be 1 but received: %d", len(resp.Choices))
	}

	result := resp.Choices[0].Message.Content
	return result, nil
}
