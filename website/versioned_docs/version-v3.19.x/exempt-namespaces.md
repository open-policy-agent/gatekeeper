---
id: exempt-namespaces
title: Exempting Namespaces
---

`Feature State`: The `Config` resource is currently alpha.

## Exempting Namespaces from Gatekeeper using config resource

> The "Config" resource must be named `config` for it to be reconciled by Gatekeeper. Gatekeeper will ignore the resource if you do not name it `config`.

The config resource can be used as follows to exclude namespaces from certain processes for all constraints in the cluster. An asterisk can be used for wildcard matching (e.g. `kube-*`). To exclude namespaces at a constraint level, use `excludedNamespaces` in the [constraint](howto.md#constraints) instead.

```yaml
apiVersion: config.gatekeeper.sh/v1alpha1
kind: Config
metadata:
  name: config
  namespace: "gatekeeper-system"
spec:
  match:
    - excludedNamespaces: ["kube-*", "my-namespace"]
      processes: ["*"]
    - excludedNamespaces: ["audit-excluded-ns"]
      processes: ["audit"]
    - excludedNamespaces: ["audit-webhook-sync-excluded-ns"]
      processes: ["audit", "webhook", "sync"]
    - excludedNamespaces: ["mutation-excluded-ns"]
      processes: ["mutation-webhook"]
...
```

Available processes:

- `audit` process exclusion will exclude resources from specified namespace(s) in audit results.
- `webhook` process exclusion will exclude resources from specified namespace(s) from the admission webhook.
- `sync` process exclusion will exclude resources from specified namespace(s) from being synced into OPA.
- `mutation-webhook` process exclusion will exclude resources from specified namespace(s) from the mutation webhook.
- `*` includes all current processes above and includes any future processes.

## Exempting Namespaces from the Gatekeeper Admission Webhook using `--exempt-namespace` flag

Note that the following only exempts resources from the admission webhook. They will still be audited. Editing individual constraints or [config resource](#exempting-namespaces-from-gatekeeper-using-config-resource) is
necessary to exclude them from audit.

If it becomes necessary to exempt a namespace from Gatekeeper webhook entirely (e.g. you want `kube-system` to bypass admission checks), here's how to do it:

   1. Make sure the validating admission webhook configuration for Gatekeeper has the following namespace selector:

        ```yaml
          namespaceSelector:
            matchExpressions:
            - key: admission.gatekeeper.sh/ignore
              operator: DoesNotExist
        ```

      the default Gatekeeper manifest should already have added this. The default name for the
      webhook configuration is `gatekeeper-validating-webhook-configuration` and the default
      name for the webhook that needs the namespace selector is `validation.gatekeeper.sh`

   2. Tell Gatekeeper it's okay for the namespace to be ignored by adding a flag to the pod:
      `--exempt-namespace=<NAMESPACE NAME>`. This step is necessary because otherwise the
      permission to modify a namespace would be equivalent to the permission to exempt everything
      in that namespace from policy checks. This way a user must explicitly have permissions
      to configure the Gatekeeper pod before they can add exemptions.

      > In order to add the `admission.gatekeeper.sh/ignore` label to a namespace, that namespace must be listed under the gatekeeper `controllerManager.exemptNamespaces` [parameter](https://github.com/open-policy-agent/gatekeeper/blob/master/charts/gatekeeper/README.md#parameters) when installing via Helm.

   3. Add the `admission.gatekeeper.sh/ignore` label to the namespace. The value attached
      to the label is ignored, so it can be used to annotate the reason for the exemption.

Similarly, you can also enable the exemption of entire groups of namespaces using the `--exempt-namespace-prefix` and `--exempt-namespace-suffix` flags. Using these flags allows the `admission.gatekeeper.sh/ignore` label to be added to any namespace that matches the supplied prefix or suffix.

## Difference between exclusion using config resource and `--exempt-namespace` flag

The difference is at what point in the admission process an exemption occurs.

If you use `--exempt-namespace` flag and `admission.gatekeeper.sh/ignore` label, Gatekeeper's webhook will not be called by the API server for any resource in that namespace. That means that Gatekeeper being down should have no effect on requests for that namespace.

If you use the config method, Gatekeeper itself evaluates the exemption. The benefit there is that we have more control over the syntax and can be more fine-grained, but it also means that the API server is still calling the webhook, which means downtime can have an impact.
