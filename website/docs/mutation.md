---
id: mutation
title: Mutation
---

`Feature State`: Gatekeeper version v3.10+ (stable)

The mutation feature allows Gatekeeper modify Kubernetes resources at request time based on customizable mutation policies.

## Mutation CRDs

Mutation policies are defined using mutation-specific CRDs, called __mutators__:
- AssignMetadata - defines changes to the metadata section of a resource
- Assign - any change outside the metadata section
- ModifySet - adds or removes entries from a list, such as the arguments to a container
- AssignImage - defines changes to the components of an image string

The rules for mutating metadata are more strict than for mutating the rest of the resource. The differences are described in more detail below.

Here is an example of a simple AssignMetadata CRD:
```yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: AssignMetadata
metadata:
  name: demo-annotation-owner
spec:
  match:
    scope: Namespaced
    name: nginx-*
    kinds:
    - apiGroups: ["*"]
      kinds: ["Pod"]
  location: "metadata.annotations.owner"
  parameters:
    assign:
      value:  "admin"
```

Each mutation CRD can be divided into 3 distinct sections:
- extent of changes - what is to be modified (kinds, namespaces, ...)
- intent - the path and value of the modification
- conditional - conditions under which the mutation will be applied

#### Extent of changes

The extent of changes section describes which resources will be mutated.
It allows selecting resources to be mutated using the same match criteria
as constraints.

An example of the extent of changes section.
```yaml
applyTo:
- groups: [""]
  kinds: ["Pod"]
  versions: ["v1"]
  operations: ["CREATE"]  # Optional: specify which operations to mutate
match:
  scope: Namespaced | Cluster
  kinds:
  - apiGroups: []
    kinds: []
  labelSelector: []
  namespaces: []
  namespaceSelector: []
  excludedNamespaces: []
```

Note that the `applyTo` field is required for all mutators except `AssignMetadata`, which does not have the `applyTo` field. 
`applyTo` allows Gatekeeper to understand the schema of the objects being modified, so that it can detect when two mutators disagree as
to a kind's schema, which can cause non-convergent mutations. Also, the `applyTo` section does not accept globs.

##### Operations Field

The `operations` field in `applyTo` allows you to specify which Kubernetes admission operations should trigger the mutation. 
This provides granular control over when mutations are applied, helping to avoid issues with immutable fields during resource updates.

```yaml
applyTo:
- groups: [""]
  kinds: ["Pod"]
  versions: ["v1"]
  operations: ["CREATE"]  # Only mutate on resource creation
```

**Supported operations:**
- `CREATE` - Apply mutation when resources are created
- `UPDATE` - Apply mutation when resources are updated  
- `DELETE` - Apply mutation when resources are deleted
- `CONNECT` - Apply mutation for connect operations (e.g., `kubectl exec`, `kubectl port-forward`)

**Backward Compatibility:** If the `operations` field is not specified or is empty, the mutation applies to all operations (CREATE, UPDATE, DELETE, and CONNECT). This is consistent with how empty `groups`, `kinds`, and `versions` fields work in `applyTo` - empty means match all.

**Common Use Cases:**
- `operations: ["CREATE"]` - Ideal for setting initial values that shouldn't change on updates (e.g., environment variables with immutable constraints)
- `operations: ["UPDATE"]` - For mutations that should only apply when resources are modified
- `operations: ["CREATE", "UPDATE"]` - Apply to both creation and updates, but not deletes or connects
- `operations: ["DELETE"]` - Advanced scenarios where cleanup mutations are needed (use with caution)
- `operations: ["CONNECT"]` - Advanced scenarios for mutating connect subresources (use with caution)

> **Important Consideration:** When using `operations: ["CREATE"]` only, the mutation will not apply to resources that already exist before the mutator is deployed. If those resources are later updated (e.g., for label changes or finalizer removal), the mutation will not be applied, and the previously mutated values remain unchanged. However, if the mutator is later deleted or modified, resources created under the old mutator may no longer match the new policy, which could cause issues if the field was set differently than what the user originally specified. Users should be aware that `operations: ["CREATE"]` effectively makes the mutated fields read-only for the mutation lifecycle.

The `match` section is common to all mutators. It supports the following match criteria:
- scope - the scope (Namespaced | Cluster) of the mutated resource
- kinds - the resource kind, any of the elements listed
- labelSelector - filters resources by resource labels listed
- namespaces - list of allowed namespaces, only resources in listed namespaces will be mutated
- namespaceSelector - filters resources by namespace selector
- excludedNamespaces - list of excluded namespaces, resources in listed namespaces will not be mutated
- name - the name of an object.  If defined, it matches against objects with the specified name.  Name also supports a prefix-based glob.  For example, `name: pod-*` matches both `pod-a` and `pod-b`.

Note that any empty/undefined match criteria are inclusive: they match any object.

#### Intent

This specifies what should be changed in the resource.

An example of the section is shown below:
```yaml
location: "spec.containers[name: foo].imagePullPolicy"
parameters:
  assign:
    value: "Always"
```

The `location` element specifies the path to be modified.
The `parameters.assign.value` element specifies the value to be set for the element specified in `location`. Note that the value can either be a simple string or a composite value.

An example of a composite value:
```yaml
location: "spec.containers[name: networking]"
parameters:
  assign:
    value:
      name: "networking"
      imagePullPolicy: Always

```

The `location` element can specify either a simple subelement or an element in a list.
For example the location `spec.containers[name:foo].imagePullPolicy` would be parsed as follows:
- ***spec**.containers[name: foo].imagePullPolicy* - the spec element
- *spec.**containers[name: foo]**.imagePullPolicy* - container subelement of spec. The container element is a list. Out of the list chosen, an element with the `name` element having the value `foo`.
 - *spec.containers[name: foo].**imagePullPolicy*** - in the element from the list chosen in the previous step the element `imagePullPolicy` is chosen

The yaml illustrating the above `location`:
```yaml
spec:
  containers:
  - name: foo
    imagePullPolicy:
```

Wildcards can be used for list element values: `spec.containers[name: *].imagePullPolicy`

##### Assigning values from metadata

*This section does not apply to ModifySet mutators*

Sometimes it's useful to assign a field's value from metadata. For instance, injecting a deployment's name into its pod template's labels
to use affinity/anti-affinity rules to [keep Pods from the same deployment on different nodes](https://github.com/open-policy-agent/feedback/discussions/15).

Assign and AssignMetadata can do this via the `fromMetadata` field. Here is an example:

```
apiVersion: mutations.gatekeeper.sh/v1
kind: AssignMetadata
metadata:
  name: demo-annotation-owner
spec:
  location: "metadata.labels.namespace"
  parameters:
    assign:
      fromMetadata:
        field: namespace
```

Valid values for `spec.parameters.assign.fromMetadata.field` are `namespace` and `name`. They will inject the namespace's name and the object's name, respectively.


##### Conditionals

The conditions for updating the resource.

Mutation has path tests, which make it so the resource will only be mutated if the specified path exists/does not exist.
This can be useful for things like setting a default value if a field is undeclared, or for avoiding creating a field
when a parent is missing, such as accidentally creating an empty sidecar named "foo" in the example below:

```yaml
parameters:
  pathTests:
  - subPath: "spec.containers[name: foo]"
    condition: MustExist
  - subPath: "spec.containers[name: foo].securityContext.capabilities"
    condition: MustNotExist
```


### AssignMetadata

AssignMetadata is a mutator that modifies the metadata section of a resource. Note that the metadata of a resource is a very sensitive piece of data,
and certain mutations could result in unintended consequences. An example of this could be changing the name or namespace of a resource.
The AssignMetadata changes have therefore been limited to only the labels and annotations. Furthermore, it is currently only allowed to add a label or annotation.
Pre-existing labels and annotations cannot be modified.

 An example of an AssignMetadata adding a label `owner` set to `admin`:
```yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: AssignMetadata
metadata:
  name: demo-annotation-owner
spec:
  match:
    scope: Namespaced
  location: "metadata.labels.owner"
  parameters:
    assign:
      value: "admin"
```

### ModifySet

ModifySet is a mutator that allows for the adding and removal of items from a list as if that list were a set.
New values are appended to the end of a list.

For example, the following mutator removes an `--alsologtostderr` argument from all containers in a pod:

```yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: ModifySet
metadata:
  name: remove-err-logging
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  location: "spec.containers[name: *].args"
  parameters:
    operation: prune
    values:
      fromList:
        - --alsologtostderr
```

- `spec.parameters.values.fromList` holds the list of values that will be added or removed.
- `operation` can be `merge` to insert values into the list if missing, or `prune` to remove values from the list. `merge` is default.

### AssignImage

AssignImage is a mutator specifically for changing the components of an image
string. Suppose you have an image like `my.registry.io:2000/repo/app:latest`.
`my.registry.io:2000` would be the domain, `repo/app` would be the path, and
`:latest` would be the tag. The domain, path, and tag of an image can be changed
separately or in conjunction.

For example, to change the whole image to `my.registry.io/repo/app@sha256:abcde67890123456789abc345678901a`:

```yaml
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: AssignImage
metadata:
  name: assign-container-image
spec:
  applyTo:
  - groups: [ "" ]
    kinds: [ "Pod" ]
    versions: [ "v1" ]
  location: "spec.containers[name:*].image"
  parameters:
    assignDomain: "my.registry.io"
    assignPath: "repo/app"
    assignTag: "@sha256:abcde67890123456789abc345678901a"
  match:
    source: "All"
    scope: Namespaced
    kinds:
    - apiGroups: [ "*" ]
      kinds: [ "Pod" ]
```

Only one of `[assignDomain, assignPath, assignTag]` is required. Note that `assignTag`
must start with `:` or `@`. Also, if `assignPath` is set to a value which could potentially
be interpreted as a domain, such as `my.repo.lib/app`, then `assignDomain` must
also be specified.

### Mutation Annotations

You can have two recording annotations applied at mutation time by enabling the `--mutation-annotations` flag. More details can be found on the
[customize startup docs page](./customize-startup.md).

## Examples

### Adding an annotation

```yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: AssignMetadata
metadata:
  name: demo-annotation-owner
spec:
  match:
    scope: Namespaced
  location: "metadata.annotations.owner"
  parameters:
    assign:
      value: "admin"
```

### Setting security context of a specific container in a Pod in a namespace to be non-privileged

Set the security context of container named `foo` in a Pod in namespace `bar` to be non-privileged

```yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: Assign
metadata:
  name: demo-privileged
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  match:
    scope: Namespaced
    kinds:
    - apiGroups: ["*"]
      kinds: ["Pod"]
    namespaces: ["bar"]
  location: "spec.containers[name:foo].securityContext.privileged"
  parameters:
    assign:
      value: false
    pathTests:
    - subPath: "spec.containers[name:foo]"
      condition: MustExist
```

### Setting imagePullPolicy of all containers to Always in all namespaces except namespace `system`

```yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: Assign
metadata:
  name: demo-image-pull-policy
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  match:
    scope: Namespaced
    kinds:
    - apiGroups: ["*"]
      kinds: ["Pod"]
    excludedNamespaces: ["system"]
  location: "spec.containers[name:*].imagePullPolicy"
  parameters:
    assign:
      value: Always
```

### Using the operations field to control mutation timing

Setting environment variables only on Pod creation to avoid issues with immutable fields:

```yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: Assign
metadata:
  name: demo-env-create-only
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
    operations: ["CREATE"]  # Only apply on resource creation
  match:
    scope: Namespaced
    kinds:
    - apiGroups: ["*"]
      kinds: ["Pod"]
  location: "spec.containers[name:*].env[name:DEPLOYMENT_TIMESTAMP].value"
  parameters:
    assign:
      value: "2024-01-01T00:00:00Z"
```

### Adding a `network` sidecar to a Pod

```yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: Assign
metadata:
  name: demo-sidecar
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  match:
    scope: Namespaced
    kinds:
    - apiGroups: ["*"]
      kinds: ["Pod"]
  location: "spec.containers[name:networking]"
  parameters:
    assign:
      value:
        name: "networking"
        imagePullPolicy: Always
        image: quay.io/foo/bar:latest
        command: ["/bin/bash", "-c", "sleep INF"]

```

### Adding dnsPolicy and dnsConfig to a Pod

```yaml
apiVersion: mutations.gatekeeper.sh/v1
kind: Assign
metadata:
  name: demo-dns-policy
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  match:
    scope: Namespaced
    kinds:
    - apiGroups: ["*"]
      kinds: ["Pod"]
  location: "spec.dnsPolicy"
  parameters:
    assign:
      value: None
---
apiVersion: mutations.gatekeeper.sh/v1
kind: Assign
metadata:
  name: demo-dns-config
spec:
  applyTo:
  - groups: [""]
    kinds: ["Pod"]
    versions: ["v1"]
  match:
    scope: Namespaced
    kinds:
    - apiGroups: ["*"]
      kinds: ["Pod"]
  location: "spec.dnsConfig"
  parameters:
    assign:
      value:
        nameservers:
        - 1.2.3.4
```

### Setting a Pod's container image to use a specific digest:

```yaml
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: AssignImage
metadata:
  name: add-nginx-digest
spec:
  applyTo:
  - groups: [ "" ]
    kinds: [ "Pod" ]
    versions: [ "v1" ]
  location: "spec.containers[name:nginx].image"
  parameters:
    assignTag: "@sha256:abcde67890123456789abc345678901a"
  match:
    source: "All"
    scope: Namespaced
    kinds:
    - apiGroups: [ "*" ]
      kinds: [ "Pod" ]
```

### External Data

See [External Data For Gatekeeper Mutating Webhook](externaldata.md#external-data-for-gatekeeper-mutating-webhook).
