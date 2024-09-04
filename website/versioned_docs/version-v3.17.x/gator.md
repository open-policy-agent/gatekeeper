---
id: gator
title: The gator CLI
---

`Feature State`: Gatekeeper version v3.11+ (beta)

The `gator` CLI is a tool for evaluating Gatekeeper ConstraintTemplates and
Constraints in a local environment.

## Installation

To install `gator`, you may either
[download the binary](https://github.com/open-policy-agent/gatekeeper/releases)
relevant to your system or build it directly from source. On macOS and Linux,
you can also install `gator` using [Homebrew](https://brew.sh).

To build from source:

```shell
go install github.com/open-policy-agent/gatekeeper/v3/cmd/gator@master
```

:::note
`go install` of `gator` requires Gatekeeper `master` branch or `v3.16.0` and later.
:::

Install with Homebrew:

```shell
brew install gator
```

## The `gator test` subcommand

`gator test` allows users to test a set of Kubernetes objects against a set of
Templates and Constraints. The command returns violations when found and
communicates success or failure via its exit status. This command will also
attempt to expand any resources passed in if a supplied `ExpansionTemplate`
matches these resources.

Note: The `gator verify` command was first called `gator test`. These names were
changed to better align `gator` with other projects in the open-policy-agent
space.

### Usage

#### Specifying inputs

`gator test` supports inputs through the `--filename` and `--image` flags, and
via stdin. The three methods of input can be used in combination or individually. The `--filename` and `--image` flags are repeatable.

The `--filename` flag can specify a single file or a directory. If a file is
specified, that file must end in one of the following extensions: `.json`,
`.yaml`, `.yml`. Directories will be walked, and any files of extensions other
than the aforementioned three will be skipped.

For example, to test a manifest (piped via stdin) against a folder of policies:

```shell
cat my-manifest.yaml | gator test --filename=template-and-constraints/
```

Or you can specify both as flags:

```shell
gator test -f=my-manifest.yaml -f=templates-and-constraints/
```

> ❗The `--image` flag is in _alpha_ stage.

The `--image` flag specifies a content addressable OCI artifact containing
policy files. The image(s) will be copied into the local filesystem in a
temporary directory, the location of which can be overridden with
the `--tempdir`
flag. Only files with the aforementioned extensions will be processed. For
information on how to create OCI policy bundles, see
the [Bundling Policy into OCI Artifacts](#bundling-policy-into-oci-artifacts)
section.

For example, to test a manifest (piped via stdin) against an OCI Artifact
containing policies:

```shell
cat my-manifest.yaml | gator test --image=localhost:5000/gator/template-library:v1 \
  --image=localhost:5000/gator/constraints:v1
```

#### Exit Codes

`gator test` will return a `0` exit status when the objects, Templates, and
Constraints are successfully ingested, no errors occur during evaluation, and no
violations are found.

An error during evaluation, for example a failure to read a file, will result in
a `1` exit status with an error message printed to stderr.

Policy violations will generate a `1` exit status as well, but violation
information will be printed to stdout.

##### Enforcement Actions

While violation data will always be returned when an object is found to be
violating a Constraint, the exit status can vary. A constraint with
`spec.enforcementAction: ""` or `spec.enforcementAction: deny` will produce a
`1` exit code, but other enforcement actions like `dryrun` will not. This is
meant to make the exit code of `1` consistent with rejection of the object by
Gatekeeper's webhook. A Constraint set to `warn` would not trigger a rejection
in the webhook, but _would_ produce a violation message. The same is true for
that constraint when used in `gator test`.

#### Output Formatting

`gator test` supports a `--output` flag that allows the user to specify a
structured data format for the violation data. This information is printed to
stdout.

The allowed values are `yaml` and `json`, specified like:

```shell
gator test --filename=manifests-and-policies/ --output=json
```

## The `gator verify` subcommand

### Writing Test Suites

`gator verify` organizes tests into three levels: Suites, Tests, and Cases:

- A Suite is a file which defines Tests.
- A Test declares a ConstraintTemplate, a Constraint, and Cases to test the
  Constraint.
- A Case defines an object to validate and whether the object is expected to
  pass validation.

Any file paths declared in a Suite are assumed to be relative to the Suite
itself. Absolute paths are not allowed. Thus, it is possible to move around a
directory containing a Suite, and the files it uses for tests.

### Suites

[An example Suite file](https://github.com/open-policy-agent/gatekeeper-library/blob/8765ec11c12a523688ed77485c7a458df84266d6/library/general/allowedrepos/suite.yaml)
.

To be valid, a Suite file must declare:

```yaml
kind: Suite
apiVersion: test.gatekeeper.sh/v1alpha1
```

`gator verify` silently ignores files which do not declare these. A Suite may
declare multiple Tests, each containing different Templates and Constraints.
Each Test in a Suite is independent.

### Tests

Each Suite contains a list of Tests under the `tests` field.

A Test compiles a ConstraintTemplate, and instantiates a Constraint for the
ConstraintTemplate. It is an error for the Constraint to have a different type
than that defined in the ConstraintTemplate spec.crd.spec.names.kind, or for the
ConstraintTemplate to not compile.

### Cases

Each Test contains a list of Cases under the `cases` field.

A Case validates an object against a Constraint. The case may specify that the
object is expected to pass or fail validation, and may make assertions about the
returned violations (if any).

A Case must specify `assertions` and whether it expects violations. The simplest
way to declare this is:

The Case expects at least one violation:

```yaml
assertions:
- violations: yes
```

The Case expects no violations:

```yaml
assertions:
- violations: no
```

Assertions contain the following fields, acting as conditions for each assertion
to check.

- `violations` is either "yes", "no", or a non-negative integer.
    - If "yes", at least one violation must otherwise match the assertion.
    - If "no", then no violation messages must otherwise match the assertion.
    - If a nonnegative integer, then exactly that many violations must match.
      Defaults to "yes".
- `message` matches violations containing the exact string specified. `message`
  is case-sensitive. If not specified or explicitly set to empty string, all
  messages returned by the Constraint are considered matching.

A Case may specify multiple assertions. For example:

```yaml
  - name: both-disallowed
    object: samples/repo-must-be-openpolicyagent/disallowed_both.yaml
    assertions:
    - violations: 2
    - message: initContainer
      violations: 1
    - message: container
      violations: 1
```

This Case specifies:

- There are exactly two violations.
- There is exactly one violation containing "initContainer".
- There is exactly one violation containing "container".

It is valid to assert that no violations match a specified message. For example,
the below is valid:

```yaml
- violations: yes
- violations: no
  message: foobar
```

This Case specifies that there is at least one violation, and no violations
contain the string "foobar".

A Case may specify `inventory`, which is a list of paths to files containing
Kubernetes objects to put in `data.inventory` for testing referential
constraints.

```yaml
inventory:
- samples/data_objects.yaml
```

More examples of working `gator verify` suites are available in the
[gatekeeper-library](https://github.com/open-policy-agent/gatekeeper-library/tree/master/library)
repository.

### Usage

To run a specific suite:

```
gator verify suite.yaml
```

To run all suites in the current directory and all child directories recursively

```shell
gator verify ./...
```

To only run tests whose full names contain a match for a regular expression, use
the `run` flag:

```shell
gator verify path/to/suites/... --run "disallowed"
```

### Validating Metadata-Based Constraint Templates

`gator verify` may be used with an [`AdmissionReview`](https://pkg.go.dev/k8s.io/kubernetes/pkg/apis/admission#AdmissionReview)
object to test your constraints. This can be helpful to simulate a certain operation (`CREATE`, `UPDATE`, `DELETE`, etc.)
or [`UserInfo`](https://pkg.go.dev/k8s.io/kubernetes@v1.25.3/pkg/apis/authentication#UserInfo) metadata.
Recall that the `input.review.user` can be accessed in the Rego code (see [Input Review](howto.md#input-review) for more guidance). The `AdmissionReview` object can be specified where you would specify the object under test above:

```yaml
  - name: both-disallowed
    object: path/to/test_admission_review.yaml
    assertions:
    - violations: 1
```

Example for testing the `UserInfo` metadata:

AdmissionReview, ConstraintTemplate, Constraint:
```yaml
kind: AdmissionReview
apiVersion: admission.k8s.io/v1beta1
request:
  operation: "UPDATE"
  userInfo:
    username: "system:foo"
  object:
    kind: Pod
    labels:
      - app: "bar"
---
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1
metadata:
  name: validateuserinfo
spec:
  crd:
    spec:
      names:
        kind: ValidateUserInfo
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8svalidateuserinfo
        violation[{"msg": msg}] {
          username := input.review.userInfo.username
          not startswith(username, "system:")
          msg := sprintf("username is not allowed to perform this operation: %v", [username])
        }
---
kind: ValidateUserInfo
apiVersion: constraints.gatekeeper.sh/v1
metadata:
  name: always-validate
```

Gator Suite:
```yaml
apiVersion: test.gatekeeper.sh/v1alpha1
kind: Suite
tests:
- name: userinfo
  template: template.yaml
  constraint: constraint.yaml
  cases:
  - name: system-user
    object: admission-review.yaml
    assertions:
    - violations: no
```

Note for `DELETE` operation, the `oldObject` should be the object being deleted:

```yaml
kind: AdmissionReview
apiVersion: admission.k8s.io/v1beta1
request:
  operation: "DELETE"
  userInfo:
    username: "system:foo"
  oldObject:
    kind: Pod
    labels:
      - app: "bar"
```

Note that [`audit`](audit.md) or `gator test` are different enforcement points and they don't have the `AdmissionReview` request metadata.

Run `gator verify --help` for more information.

## The `gator expand` subcommand

`gator expand` allows users to test the behavior of their Expansion configs. The
command accepts a file or directory containing the expansion configs, which
should include the resource(s) under test, the `ExpansionTemplate`(s), and
optionally any Mutation CRs. The command will output a manifest containing the
expanded resources.

If the mutators or constraints use `spec.match.namespaceSelector`, the namespace the resource
belongs to must be supplied in order to correctly evaluate the match criteria.
If a resource is specified for expansion but its non-default namespace is not
supplied, the command will exit 1. See the [non default namespace example](#non-default-namespace-example) below.

### Usage

Similar to `gator test`, `gator expand` expects a `--filename` or `--image`
flag. The flags can be used individually, in combination, and/or repeated.

```shell
gator expand --filename="manifest.yaml" –filename="expansion-policy/"
```

Or, using an OCI Artifact for the expansion configuration:

```shell
gator expand --filename="my-deployment.yaml" --image=localhost:5000/gator/expansion-policy:v1
```

By default, `gator expand` will output to stdout, but a `–outputfile` flag can be
specified to write the results to a file.

```shell
gator expand --filename="manifest.yaml" –outputfile="results.yaml"
```

`gator expand` can output in `yaml` or `json` (default is `yaml`).

```shell
gator expand --filename="manifest.yaml" –format="json"
```

See `gator expand –help` for more details. `gator expand` will exit 1 if there
is a problem parsing the configs or expanding the resources.

#### Non default namespace example

This is an example setup where we include a `namespace` in a `manifest.yaml` that we plan on passing to `gator expand`.

```yaml
apiVersion: expansion.gatekeeper.sh/v1alpha1
kind: ExpansionTemplate
metadata:
  name: expand-deployments
spec:
  applyTo:
  - groups: [ "apps" ]
    kinds: [ "Deployment" ]
    versions: [ "v1" ]
  templateSource: "spec.template"
  generatedGVK:
    kind: "Pod"
    group: ""
    version: "v1"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  namespace: my-ns
  labels:
    app: nginx
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: 80
        args:
        - "/bin/sh"
---
apiVersion: mutations.gatekeeper.sh/v1alpha1
kind: Assign
metadata:
  name: always-pull-image
spec:
  applyTo:
  - groups: [ "" ]
    kinds: [ "Pod" ]
    versions: [ "v1" ]
  location: "spec.containers[name: *].imagePullPolicy"
  parameters:
    assign:
      value: "Always"
  match:
    source: "Generated"
    scope: Namespaced
    kinds:
    - apiGroups: [ ]
      kinds: [ ]
    namespaceSelector:
      matchExpressions:
        - key: admission.gatekeeper.sh/ignore
          operator: DoesNotExist
---
# notice this file is providing the non default namespace `my-ns`
apiVersion: v1
kind: Namespace
metadata:
  name: my-ns
```

Calling `gator expand --filename=manifest.yaml` will produce the following output:

```yaml
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: nginx
  name: nginx-deployment-pod
  namespace: my-ns
spec:
  containers:
  - args:
    - /bin/sh
    image: nginx:1.14.2
    imagePullPolicy: Always
    name: nginx
    ports:
    - containerPort: 80
```

However, not including the `namespace` definition in the call to `gator expand` will exit with a status code of 1 and error out with:

```
error expanding resources: error expanding resource nginx-deployment: failed to mutate resultant resource nginx-deployment-pod: matching for mutator Assign.mutations.gatekeeper.sh /always-pull-image failed for  Pod my-ns nginx-deployment-pod: failed to run Match criteria: namespace selector for namespace-scoped object but missing Namespace
```

## Bundling Policy into OCI Artifacts

It may be useful to bundle policy files into OCI Artifacts for ingestion during
CI/CD workflows. The workflow could perform validation on inbound objects using
`gator test|expand`.

A policy bundle can be composed of any arbitrary file structure, which `gator`
will walk recursively. Any files that do not end in `json|yaml|yml` will be
ignored. `gator` does not enforce any file schema in the artifacts; it only
requires that all files of the support extensions describe valid Kubernetes
resources.

We recommend using the [Oras CLI](https://oras.land/cli/) to create OCI
artifacts. For example, to push a bundle containing the 2 local directories
`constraints` and `template_library`:

```shell
oras push localhost:5000/gator/policy-bundle:v1 ./constraints/:application/vnd.oci.image.layer.v1.tar+gzip \
  ./template_library/:application/vnd.oci.image.layer.v1.tar+gzip
```

This expects that the `constraints` and `template_library` directories are at
the path that this command is being run from.

## Gotchas

### Duplicate violation messages

Rego de-duplicates identical violation messages. If you want to be sure that a
test returns multiple violations, use a unique message for each violation.
Otherwise, if you specify an exact number of violations, the test may fail.

### Matching is case-sensitive

Message declarations are case-sensitive. If a test fails, check that the
expected message's capitalization exactly matches the one in the template.

### Referential constraints and Namespace-scoped resources

Gator cannot determine if a type is Namespace-scoped or not, so it does not
assign objects to the default Namespace automatically. Always specify
`metadata.namespace` for Namespace-scoped objects to prevent test failures, or
to keep from specifying templates which will fail in a real cluster.

## Platform Compatibility

`gator` is only automatically tested on Linux for each commit. If you want to
use `gator` on other systems, let us know by replying to
[this issue](https://github.com/open-policy-agent/gatekeeper/issues/1655).

`gator verify` has been manually tested on Windows and works as of
[this commit](https://github.com/open-policy-agent/gatekeeper/commit/b3ed94406583c85f3102c54a32f362d27f76da96)
. Continued functionality is not guaranteed.

File paths which include backslashes are not portable, so suites using such
paths will not work as intended on Windows.
