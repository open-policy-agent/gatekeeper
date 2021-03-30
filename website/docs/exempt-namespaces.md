---
id: exempt-namespaces
title: Exempting Namespaces
---

## Exempting Namespaces from Gatekeeper using config resource

The config resource can be used as follows to exclude namespaces from certain processes for all constraints in the cluster. To exclude namespaces at a constraint level, use `excludedNamespaces` in the [constraint](howto.md#constraints) instead.

```yaml
apiVersion: config.gatekeeper.sh/v1alpha1
kind: Config
metadata:
  name: config
  namespace: "gatekeeper-system"
spec:
  match:
    - excludedNamespaces: ["kube-system", "gatekeeper-system"]
      processes: ["*"]
    - excludedNamespaces: ["audit-excluded-ns"]
      processes: ["audit"]
    - excludedNamespaces: ["audit-webhook-sync-excluded-ns"]
      processes: ["audit", "webhook", "sync"]
...
```

Available processes:
- `audit` process exclusion will exclude resources from specified namespace(s) in audit results.
- `webhook` process exclusion will exclude resources from specified namespace(s) from the admission webhook.
- `sync` process exclusion will exclude resources from specified namespace(s) from being synced into OPA.
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

   3. Add the `admission.gatekeeper.sh/ignore` label to the namespace. The value attached
      to the label is ignored, so it can be used to annotate the reason for the exemption.
