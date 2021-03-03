# Gatekeeper

## How is Gatekeeper different from OPA?

Compared to using [OPA with its sidecar kube-mgmt](https://www.openpolicyagent.org/docs/kubernetes-admission-control.html) (aka Gatekeeper v1.0), Gatekeeper introduces the following functionality:

   * An extensible, parameterized policy library
   * Native Kubernetes CRDs for instantiating the policy library (aka "constraints")
   * Native Kubernetes CRDs for extending the policy library (aka "constraint templates")
   * Audit functionality

## Getting started

Check out the [installation instructions](https://open-policy-agent.github.io/gatekeeper/website/docs/install) to deploy Gatekeeper components to your Kubernetes cluster.

## Documentation

Please see the [docs](https://open-policy-agent.github.io/gatekeeper/website/docs/howto) for more in-depth information.

## Policy Library

See the [Gatekeeper policy library](https://www.github.com/open-policy-agent/gatekeeper-library) for a collection of constraint templates and sample constraints that you can use with Gatekeeper.

## Community

Join us to help define the direction and implementation of this project!

- Join the [`#kubernetes-policy`](https://openpolicyagent.slack.com/messages/CDTN970AX)
  channel on [OPA Slack](https://slack.openpolicyagent.org/).

- Join [weekly meetings](https://docs.google.com/document/d/1A1-Q-1OMw3QODs1wT6eqfLTagcGmgzAJAjJihiO3T48/edit)
  to discuss development, issues, use cases, etc.

- Use [GitHub Issues](https://github.com/open-policy-agent/gatekeeper/issues)
  to file bugs, request features, or ask questions asynchronously.

## Code of conduct

This project is governed by the [CNCF Code of conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md).

## Security

Please report vulnerabilities by email to [open-policy-agent-security](mailto:open-policy-agent-security@googlegroups.com).
We will send a confirmation message to acknowledge that we have received the
report and then we will send additional messages to follow up once the issue
has been investigated.

For details on the security release process please refer to the [open-policy-agent/opa/SECURITY.md](https://github.com/open-policy-agent/opa/blob/master/SECURITY.md) file.
