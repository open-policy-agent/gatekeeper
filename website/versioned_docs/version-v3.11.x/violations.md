---
id: violations
title: Handling Constraint Violations
---

## Log denies

Set the `--log-denies` flag to log all deny, dryrun and warn failures.
This is useful when trying to see what is being denied/fails dry-run and keeping a log to debug cluster problems without having to enable syncing or looking through the status of all constraints.

## Dry Run enforcement action

When rolling out new constraints to running clusters, the dry run functionality can be helpful as it enables constraints to be deployed in the cluster without making actual changes. This allows constraints to be tested in a running cluster without enforcing them. Cluster resources that are impacted by the dry run constraint are surfaced as violations in the `status` field of the constraint.

To use the dry run feature, add `enforcementAction: dryrun` to the constraint spec to ensure no actual changes are made as a result of the constraint. By default, `enforcementAction` is set to `deny` as the default behavior is to deny admission requests with any violation.

For example:
```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: ns-must-have-gk
spec:
  enforcementAction: dryrun
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Namespace"]
  parameters:
    labels: ["gatekeeper"]
status:
  auditTimestamp: "2019-08-15T01:46:13Z"
  enforced: true
  violations:
  - enforcementAction: dryrun
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: default
  - enforcementAction: dryrun
    kind: Namespace
    message: 'you must provide labels: {"gatekeeper"}'
    name: gatekeeper-system

```

## Warn enforcement action

Warn enforcement action offers the same benefits as dry run, such as testing constraints without enforcing them. In addition to this, it will also provide immediate feedback on why that constraint would have been denied. It is available in Gatekeeper v3.4+ with Kubernetes v1.19+.

```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sAllowedRepos
metadata:
  name: repo-is-openpolicyagent
spec:
  enforcementAction: warn
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
  parameters:
    repos:
      - "openpolicyagent"
```

```shell
$ kubectl apply pod.yaml
Warning: [prod-repo-is-openpolicyagent] container <nginx> has an invalid image repo <nginx>, allowed repos are ["openpolicyagent"]
pod/pause created
```


> NOTE: The supported enforcementActions are [`deny`, `dryrun`, `warn`] for constraints. Update the `--disable-enforcementaction-validation=true` flag if the desire is to disable enforcementAction validation against the list of supported enforcementActions.
