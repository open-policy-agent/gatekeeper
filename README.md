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

Please see the [Gatekeeper website](https://open-policy-agent.github.io/gatekeeper/website/docs/howto) for more in-depth information.

## Policy Library

See the [Gatekeeper policy library](https://www.github.com/open-policy-agent/gatekeeper-library) for a collection of constraint templates and sample constraints that you can use with Gatekeeper.

## Community & Contributing

Please refer to [Gatekeeper's contribution guide](https://open-policy-agent.github.io/gatekeeper/website/docs/help) to find out how you can help.

## Code of conduct

This project is governed by the [CNCF Code of conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md).

## Security

For details on how to report vulnerabilities and security release process, please refer to [Gatekeeper Security](https://open-policy-agent.github.io/gatekeeper/website/docs/security) for more information.
