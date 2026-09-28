---
id: debug
title: Debugging
---

> NOTE: Verbose logging with DEBUG level can be turned on with `--log-level=DEBUG`.  By default, the `--log-level` flag is set to minimum log level `INFO`. Acceptable values for minimum log level are [`DEBUG`, `INFO`, `WARNING`, `ERROR`]. In production, this flag should not be set to `DEBUG`.

## Viewing the Request Object

A simple way to view the request object is to use a constraint/template that
denies all requests and outputs the request object as its rejection message.

Example template:

```yaml
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8sdenyall
spec:
  crd:
    spec:
      names:
        kind: K8sDenyAll
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdenyall

        violation[{"msg": msg}] {
          msg := sprintf("REVIEW OBJECT: %v", [input.review])
        }
```

Example constraint:

```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sDenyAll
metadata:
  name: deny-all-namespaces
spec:
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Namespace"]
```

## Tracing

In debugging decisions and constraints, a few pieces of information can be helpful:

   * Cached data and existing rules at the time of the request
   * A trace of the evaluation
   * The input document being evaluated

Writing out all the information above for every request would be expensive in terms of memory and load on the Gatekeeper server, which may lead to requests timing out. It would also be hard
to find the relevant logs for a given request.

For tracing, Gatekeeper **requires** operators to specify
resources and requesting users for which traces will be logged. They can do so by
configuring the `Config` resource, which lives in the `gatekeeper-system` namespace.

Below is an example of a config resource:

```yaml
apiVersion: config.gatekeeper.sh/v1alpha1
kind: Config
metadata:
  name: config
  namespace: "gatekeeper-system"
spec:
  # Data to be replicated into OPA
  sync:
    syncOnly:
      - group: ""
        version: "v1"
        kind: "Namespace"
  validation:
    # Requests for which we want to run traces
    traces:
        # The requesting user for which traces will be run
        # This field is required.
        # To trace multiple users, feel free to pass in a list.
        # To trace controllers, use the service accounts of those controllers.
      - user: "user_to_trace@company.com"
        kind:
          # The group, version, kind for which we want to run a trace
          group: ""
          version: "v1"
          kind: "Namespace"
        # If dump is defined and set to `All`, also dump the state of OPA
        dump: "All"
```

Traces will be written to the stdout logs of the Gatekeeper controller.


If there is an error in the Rego in the ConstraintTemplate, there are cases where it is still created via `kubectl apply -f [CONSTRAINT_TEMPLATE_FILENAME].yaml`.

When applying the constraint using `kubectl apply -f constraint.yaml` with a ConstraintTemplate that contains incorrect Rego, and error will occur: `error: unable to recognize "[CONSTRAINT_FILENAME].yaml": no matches for kind "[NAME_OF_CONSTRAINT]" in version "constraints.gatekeeper.sh/v1beta1"`.

To find the error, run `kubectl get -f [CONSTRAINT_TEMPLATE_FILENAME].yaml -o yaml`. Build errors are shown in the `status` field.