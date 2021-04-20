---
id: mutation
title: Mutation
---

The mutation feature allows Gatekeeper to not only validate created Kubernetes resources but also modify them based on defined mutation policies.
The feature is still in an alpha stage, so the final form can still change.

Status: alpha

## Mutation CRDs

The mutation policies are defined by means of mutation specific CRDs:
- AssignMetadata - defines changes to the metadata section of a resource
- Assign - any change outside the metadata section

The rules of mutating the metadata section are more strict than for mutating the rest of the resource definition. The differences will be described in more detail below.

Here is an example of a simple AssignMetadata CRD:
```yaml
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: AssignMetadata
metadata:
  name: demo-annotation-owner
spec:
  match:
    scope: Namespaced
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

The extent of changes section describes the resource which will be mutated.
It allows to filter the resources to be mutated by kind, label and namespace.

An example of the extent of changes section.
```yaml
applyTo:
- groups: [""]
  kinds: ["Pod"]
  versions: ["v1"]
match:
  scope: Namespaced | Cluster
  kinds:
  - APIGroups: []
    kinds: []
  labelSelector: []
  namespaces: []
  namespaceSelector: []
  excludedNamespaces: []
```

Note that the `applyTo` section applies to the Assign CRD only. It allows filtering of resources by the resource GVK (group version kind). Note that the `applyTo` section does not accept globs.

The `match` section is common to both Assign and AssignMetadata. It supports the following elements:
- scope - the scope (Namespaced | Cluster) of the mutated resource
- kinds - the resource kind, any of the elements listed
- labelSelector - filters resources by resource labels listed
- namespaces - list of allowed namespaces, only resources in listed namespaces will be mutated
- namespaceSelector - filters resources by namespace selector
- excludedNamespaces - list of excluded namespaces, resources in listed namespaces will not be mutated

Note that the resource is not filtered if an element is not present or an empty list.

#### Intent

This specifies what should be changed in the resource.

An example of the section is shown below:
```yaml
location: "spec.containers[name:foo].imagePullPolicy"
parameters:
  assign:
    value: "Always"
```

The `location` element specifies the path to be modified.
The `parameters.assign.value` element specifies the value to be set for the element specified in `location`. Note that the value can either be a simple string or a composite value.

An example of a composite value:
```yaml
location: "spec.containers[name:networking]"
parameters:
  assign:
    value:
      name: "networking"
      imagePullPolicy: Always

```

The `location` element can specify either a simple subelement or an element in a list.
For example the location `spec.containers[name:foo].imagePullPolicy` would be parsed as follows:
- ***spec**.containers[name:foo].imagePullPolicy* - the spec element
- *spec.**containers[name:foo]**.imagePullPolicy* - container subelement of spec. The container element is a list. Out of the list chosen, an element with the `name` element having the value `foo`.
 - *spec.containers[name:foo].**imagePullPolicy*** - in the element from the list chosen in the previous step the element `imagePullPolicy` is chosen

The yaml illustrating the above `location`:
```yaml
spec:
  containers:
  - name: foo
    imagePullPolicy:
```

Wildcards can be used for list element values: `spec.containers[name:*].imagePullPolicy`


##### Conditionals

The conditions for updating the resource.
Two types of conditions exist:
- path tests - a resource will only be updated when a specified path exists or not
- value tests - a resource will only be updated when the existing value is/is not contained in a list of values

An example of the conditionals: 
```yaml
parameters:
  pathTests:
  - subPath: "spec.containers[name:foo]"
    condition: MustExist
  - subPath: spec.containers[name:foo].securityContext.capabilities
    condition: MustNotExist

  assignIf:
    in: [<value 1>, <value 2>, <value 3>, ...]
    notIn: [<value 1>, <value 2>, <value 3>, ...]

```


### AssignMetadata

AssignMetadata is a CRD for modifying the metadata section of a resource. Note that the metadata of a resource is a very sensitive piece of data, and certain mutations could result in unintended consequences. An example of this could be changing the name or namespace of a resource. The AssignMetadata changes have therefore been limited to only the labels and annotations. Furthermore, it is currently only allowed to add a label or annotation.
 
 An example of an AssignMetadata adding a label `owner` set to `admin`:
```yaml
apiVersion: mutations.gatekeeper.sh/v1alpha1
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

## Examples

### Adding an annotation

```yaml
apiVersion: mutations.gatekeeper.sh/v1alpha1
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
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: demo-privileged
  namespace: default
spec:
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
```

#### Setting imagePullPolicy of all containers to Always in all namespaces except namespace `system`

```yaml
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: demo-image-pull-policy
  namespace: default
spec:
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

### Adding a `network` sidecar to a Pod

```yaml
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: demo-sidecar
  namespace: default
spec:
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
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: demo-dns-policy
  namespace: default
spec:
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
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: demo-dns-config
  namespace: default
spec:
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
