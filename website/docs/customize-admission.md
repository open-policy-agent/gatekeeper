---
id: customize-admission
title: Customizing Admission Behavior
---

Gatekeeper is a [Kubernetes admission webhook](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#webhook-configuration)
whose default configuration can be found in the `gatekeeper.yaml` manifest file. By default, it is
a `ValidatingWebhookConfiguration` resource named `gatekeeper-validating-webhook-configuration`.

Currently the configuration specifies two webhooks: one for checking a request against
the installed constraints and a second webhook for checking labels on namespace requests
that would result in bypassing constraints for the namespace. The namespace-label webhook
is necessary to prevent a privilege escalation where the permission to add a label to a
namespace is equivalent to the ability to bypass all constraints for that namespace.
You can read more about the ability to exempt namespaces by label [here](exempt-namespaces.md#exempting-namespaces-from-the-gatekeeper-admission-webhook-using---exempt-namespace-flag).

Because Kubernetes adds features with each version, if you want to know how the webhook can be configured it
is best to look at the official documentation linked at the top of this section. However, two particularly important
configuration options deserve special mention: [timeouts](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#timeouts) and
[failure policy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy).

Timeouts allow you to configure how long the API server will wait for a response from the admission webhook before it
considers the request to have failed. Note that setting the timeout longer than the overall request timeout
means that the main request will time out before the webhook's failure policy is invoked.

Failure policy controls what happens when a webhook fails for whatever reason. Common
failure scenarios include timeouts, a 5xx error from the server or the webhook being unavailable.
You have the option to ignore errors, allowing the request through, or failing, rejecting the request.
This results in a direct tradeoff between availability and enforcement.

Currently Gatekeeper is defaulting to using `Ignore` for the constraint requests. This is because
the webhook server currently only has one instance, which risks downtime during actions like upgrades.
As the theoretical availability improves we will likely change the default to `Fail`.

The namespace label webhook defaults to `Fail`, this is to help ensure that policies preventing
labels that bypass the webhook from being applied are enforced. Because this webhook only gets
called for namespace modification requests, the impact of downtime is mitigated, making the
theoretical maximum availability less of an issue.

Because the manifest is available for customization, the webhook configuration can
be tuned to meet your specific needs if they differ from the defaults.

## Enable Delete Operations

To enable Delete operations for the `validation.gatekeeper.sh` admission webhook, add "DELETE" to the list of operations in the `gatekeeper-validating-webhook-configuration` ValidatingWebhookConfiguration as seen in this deployment manifest of gatekeeper: [here](https://github.com/open-policy-agent/gatekeeper/blob/v3.1.0-beta.10/deploy/gatekeeper.yaml#L792-L794)
Note: For admission webhooks registered for DELETE operations, use Kubernetes v1.15.0+

 So you have
 ```YAML
    operations:
    - CREATE
    - UPDATE
    - DELETE
```

You can now check for deletes.
