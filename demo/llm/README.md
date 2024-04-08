> [!WARNING]
> This is a demo of a prototype-stage feature and is subject to change. Feedback is welcome!

> [!NOTE]
> LLM engine can be used in addition to Rego and CEL/K8s Native Validation drivers. It is not a replacement for Rego or CEL.
>
> Depending on your provider, LLM engine may have additional costs. Please refer to your provider's pricing details for more information.

## Pre-requisites

- Supports [OpenAI](https://platform.openai.com), [Azure OpenAI](https://azure.microsoft.com/en-us/products/ai-services/openai-service), and any other LLM inference engine that supports the OpenAI API, such as [AIKit](https://sozercan.github.io/aikit/) or others. With open weights models, depending on the model, results may not be optimal.
- For OpenAI and Azure OpenAI, you need GPT 3.5 or GPT 4 (recommended) with a minimum of `1106` or `0125` and later versions.

## Gatekeeper
- Requires [building Gatekeeper from source](https://open-policy-agent.github.io/gatekeeper/website/docs/install#deploying-head-using-make) and deploying to a Kubernetes cluster.
- Set `--experimental-enable-llm-engine` in Gatekeeper deployments.
- Depending on the endpoint, you might need to update validating webhook configuration timeout value.
- Create or edit the secret called `gatekeeper-openai-secret` in Gatekeeper's namespace (`gatekeeper-system` by default)
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: gatekeeper-openai-secret
  namespace: gatekeeper-system
data:
  openai-api-key: # base64 encoded openai api key
  openai-deployment-name: # base64 encoded openai model or deployment name
  openai-endpoint: # base64 encoded openai or openai api compatible endpoint. Defaults to https://api.openai.com/v1 if not set.
```

## Gator
- Requires building Gator from source with `make gator`.
- Set the following environment variables:
```shell
export OPENAI_API_KEY= # Your OpenAI API key.
export OPENAI_DEPLOYMENT_NAME= # The deployment or model name used for OpenAI.
export OPENAI_ENDPOINT= # The endpoint for OpenAI service. Defaults to https://api.openai.com/v1. Set this Azure OpenAI Service or OpenAI API compatible endpoint, if needed.
```
- Run Gator with the `--experimental-enable-llm-engine` flag.

```shell
$ cat example.yaml | bin/gator test --experimental-enable-llm-engine -f demo/llm
v1/Pod nginx: ["repo-is-dockerio"] Message: "Pod's container is using an image from a disallowed registry: docker.io"
```

## Demo
[![asciicast](https://asciinema.org/a/QTBEBp8l0vE5wgD9yGgxE3mnV.svg)](https://asciinema.org/a/QTBEBp8l0vE5wgD9yGgxE3mnV)
