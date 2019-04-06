# OPA Constraint Framework

## Introduction

### What is a Constraint?

A constraint is a declaration that its author wants a system to meet a given set of
requirements. For example, if I have a system with objects that can be labeled and
I want to make sure that every object has a `billing` label, I might write the
following constraint YAML:

```yaml
apiVersion: constraints.gatekeeper.sh/v1alpha1
kind: FooSystemRequiredLabel
metadata:
  name: require-billing-label
spec:
  match:
    namespace: ["expensive"]
  parameters:
    labels: ["billing"]
```

Once this constraint is enforced, all objects in the `expensive` namespace will be
required to have a `billing` label.

### What is an Enforcement Point?

Enforcement Points are places where constraints can be enforced. Examples are Git
hooks and Kubernetes admission controllers and audit systems. The goal of this
project is to make it easy to take a common set of constraints and apply them to
multiple places in a workflow, improving likelihood of compliance.

### What is a Constraint Template?

Constraint Templates allow people to declare new constraints. They can provide the
expected input parameters and the underlying Rego necessary to enforce their
intent. For example, to define the `FooSystemRequiredLabel` constraint kind
implemented above, I might write the following template YAML:

```yaml
apiVersion: gatekeeper.sh/v1alpha1
kind: ConstraintTemplate
metadata:
  name: foosystemrequiredlabels
spec:
  crd:
    spec:
      names:
        kind: FooSystemRequiredLabel
        listKind: FooSystemRequiredLabelsList
        plural: foosystemrequiredlabels
        singular: foosystemrequiredlabel
      validation:
        # Schema for the `parameters` field
        openAPIV3Schema:
          properties:
            labels:
              type: array
              items: string
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
deny[{"msg": msg, "details": {"missing_labels": missing}}] {
   provided := {label | input.request.object.metadata.labels[label]}
   required := {label | label := input.constraint.spec.parameters.labels[_]}
   missing := required - provided
   count(missing) > 0
   msg := sprintf("you must provide labels: %v", [missing])
}
```

The most important pieces of the above YAML are:

   * `validation`, which provides the schema for the `parameters` field for the constraint
   * `targets`, which specifies what "target" (defined later) the constraint applies to. Note
      that currently constraints can only apply to one target.
   * `rego`, which defines the logic that enforces the constraint.
 
 #### Rego Semantics for Constraints
 
 There are a few rules for the Rego constraint source code:
 
   1. Everything is contained in one package
   2. Limited external data access
      * No imports
      * Only certain subfields of the `data` object can be accessed:
        * `data.inventory` allows access to the cached objects for the current target
      * Full access to the `input` object
   3. Specific rule signature schema (described below)
 
##### Rule Schema

While template authors are free to include whatever rules and functions they wish
to support their constraint, the main entry point called by the framework has a
specific signature:

```rego
deny[{"msg": msg, "details": {}}] {
  # rule body
}
```

  * The rule name must be `deny`
  * `msg` is the string message returned to the violator. It is required.
  * `details` allows for custom values to be returned. This helps support uses like
     automated remediation. There is no predefined schema for the `details` object. 
     Returning `details` is optional.
 
### What is a Target?
 
Target is an abstract concept. It represents a coherent set of objects sharing a
common identification and/or selection scheme, generic purpose, and can be analyzed
in the same validation context. This is probably best illustrated by a few examples.

#### Examples
 
##### Kubernetes Admission Webhooks Create a Target
 
All Kubernetes resources are defined by `group`, `version` and `kind`. They can
additionally be grouped by namespace, or by using label selectors. Therefore they
have a common naming and selection scheme. All Kubernetes resources declaratively
configure the state of a Kubernetes cluster, therefore they share a purpose.
Finally, they are all can be evaluated using a [Validating Admission Webhook](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/).
Therefore, they have a common validation context. These three properties make
Kubernetes admission webhooks a potential target.
 
##### Kubernetes Authorization Webhooks Create a Target
 
All Kubernetes requests are can be defined by their type (e.g. `CREATE`, `UPDATE`,
`WATCH`) and therefore have a common selection scheme. All Kubernetes requests
broadcast the requestor's intent to modify the Kubernetes cluster. Therefore they
have a common purpose. All requests can be evaluated by an [authorization webhook](https://kubernetes.io/docs/reference/access-authn-authz/webhook/)
and therefore they share a common evaluation schema.
 
#### How Do I Know if [X] Should be a Target?
 
Currently there are no hard and fast litmus tests for determining a good boundary
for a target, much like there are no hard and fast rules for what should be in a
function or a class, just guidelines, ideology and the notion of orthoganality and
testability (among others). Chances are, if you can come up with a set of rules for
a new system that could be useful, you may have a good candidate for a new target.

## Creating a New Target

Targets have a relatively simple interface:

```go
type TargetHandler interface {
	
	// The name of a target. Must match `^[a-zA-Z][a-zA-Z0-9.]*$`
	GetName() string
	
	// MatchSchema returns the JSON Schema for the `match` field of a constraint
	MatchSchema() apiextensionsv1beta1.JSONSchemaProps
	
	// Library returns the pieces of Rego code required to stitch together constraint evaluation  // for the target. Current required libraries are `matching_constraints` and
	// `matching_reviews_and_constraints` 
	// 
	// Libraries are currently templates that have the following parameters:
	//   ConstraintsRoot: The root path under which all constraints for the target are stored 
	//   DataRoot: The root path under which all data for the target is stored 
	Library() *template.Template 

	// ProcessData takes a potential data object and returns: 
	//   true if the target handles the data type 
	//   the path under which the data should be stored in OPA 
	//   the data in an object that can be cast into JSON, suitable for storage in OPA 
	ProcessData(interface{}) (bool, string, interface{}, error) 

	// HandleReview takes a potential review request and builds the `review` field of the input
	// object. it returns: 
	// 		true if the target handles the data type 
	// 		the data for the `review` field 
	HandleReview(interface{}) (bool, interface{}, error) 
	
	// HandleViolation allows for post-processing of the result object, which can be mutated directly
	HandleViolation(result *types.Result) error
	
	// ValidateConstraint returns if the constraint is misconfigured in any way. This allows for
	// non-trivial validation of things like match schema
	ValidateConstraint(*unstructured.Unstructured) error
}
```

The two most interesting fields here are `HandleReview()`, `MatchSchema()`, and `Library()`.

### `HandleReview()`

`HandleReview()` determinines whether and how a target handler is involved with a
`Review()` request (which checks to make sure an input complies with all
constraints). It returns `true` if the target should be involved with reviewing the
object and the second return value defines the schema of the `input.review` object
available to all constraint rules.

### `MatchSchema()`

`MatchSchema()` tells the system the schema for the `match` field of every
constraint using the target handler. It uses the same schema as Kubernetes' [Custom Resource Definitions](https://kubernetes.io/docs/tasks/access-kubernetes-api/custom-resources/custom-resource-definitions/).

### `Library()`

`Library()` is a hook that lets the target handler express the relationship between
constraints, input data, and cached data. The target handler must return a Golang
text template that forms a Rego module with at least two rules:

   * `matching_constraints[constraint]`
      * Returns all `constraint` objects that satisfy the `match` criteria for
        a given `input`. This `constraint` will be assigned to `input.constraint`.
   * `matching_reviews_and_constraints[[review, constraint]]`
      * Returns a `review` that corresponds to all cached data for the target. It
        also returns a `constraint` for every constraint relevant to a review.
        Values will be made available to constraint rules as `input.constraint` and
        `input.review`.
   
Note that the `Library()` module will be sandboxed much like how constraint rules
are sandboxed. With the following additional freedoms:

   * `data.constraints` is available
   * `data.external` is available
   
To make it easier to write these rules and to allow the framework to
transparently change its data layout without requiring redevelopment work
by target authors, the following template variables are provided:

   * `ConstraintsRoot` references the root of the constraints tree for the target. Beneath this root, constraints are organized by `kind` and `metadata.name`
   * `DataRoot` references the root of the data tree for the target. Beneath 
   this root, objects are stored under the path provided by `ProcessData()`.
   
## Integrating With an Enforcement Point

To effectively run reviews and audits, enforcement points need to be able to perform the
following tasks:

   * Add/Remove templates
   * Add/Remove constraints
   * Add/Remove cached data
   * Submit an object for a review
   * Request an audit of the cached data
   
To facilitate these tasks, the framework provides the following Client interface:

```go
type Client interface {
	AddData(context.Context, interface{}) (*types.Responses, error)
	RemoveData(context.Context, interface{}) (*types.Responses, error)

	CreateCRD(context.Context, *v1alpha1.ConstraintTemplate) (*apiextensionsv1beta1.CustomResourceDefinition, error)
	AddTemplate(context.Context, *v1alpha1.ConstraintTemplate) (*types.Responses, error)
	RemoveTemplate(context.Context, *v1alpha1.ConstraintTemplate) (*types.Responses, error)

	AddConstraint(context.Context, *unstructured.Unstructured) (*types.Responses, error)
	RemoveConstraint(context.Context, *unstructured.Unstructured) (*types.Responses, error)
	ValidateConstraint(context.Context, *unstructured.Unstructured) error

	// Reset the state of OPA
	Reset(context.Context) error

	// Review makes sure the provided object satisfies all stored constraints
	Review(context.Context, interface{}) (*types.Responses, error)

	// Audit makes sure the cached state of the system satisfies all stored constraints
	Audit(context.Context) (*types.Responses, error)

	// Dump dumps the state of OPA to aid in debugging
	Dump(context.Context) (string, error)
}
```

`AddTemplate()` has a unique signature because it returns the Kubernetes Custom
Resource Definition that can allow for the creation of constraints once registered.

Requests to a client will be multiplexed to all registered targets. Those targets who self-report
as being able to handle the request will all be able to add response values.

`types.Responses` is a wrapper around zero to multiple `Result` objects. Each result
object has the following fields:

```go
type Result struct {
	Msg string `json:"msg,omitempty"`

	// Metadata includes the contents of `details` from the Rego rule signature
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// The constraint that was violated
	Constraint *unstructured.Unstructured `json:"constraint,omitempty"`

	// The violating review
	Review interface{} `json:"review,omitempty"`

	// The violating Resource, filled out by the Target
	Resource interface{}
}
```

### Instantiating a Client

Here's how to create a client to make use of the framework:

```go
// local is the local driver package
driver := local.New()

backend, err := client.Backend(client.Driver(driver))
if err != nil {
	return err
}

cl, err := backend.NewClient(client.Targets(target1, target2, target3))

// cl is now available to be called as-necessary
```

### Local and Remote Clients

There are two types of clients. The local client creates an in-process instance of OPA
to respond to requests. The remote client dials an external OPA instance
and makes requests via HTTP/HTTPS.

### Debugging

There are two helpful endpoints for debugging:

   * `Client.Dump()` returns all data cached in OPA and every module created in OPA
   * Drivers can be initialized with a tracing option like so: `local.New(local.Tracing(true))`.
     These traces can then be viewed by calling `TraceDump()` on the response.