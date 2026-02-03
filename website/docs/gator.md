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

:::note
Flag `enable-k8s-native-validation` enables ConstraintTemplate containing "validating admission policy styled CEL". By default, this flag is enabled and set to `true`.
:::

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

The `--deny-only` flag will only output violations about denied constraints, not the ones using `warn` enforcement action.

:::note
`--deny-only` flag is available after Gatekeeper 3.19.
:::


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

:::note
Flag `enable-k8s-native-validation` enables ConstraintTemplate containing "validating admission policy styled CEL". By default, this flag is enabled and set to `true`.
:::

### Writing Test Suites

`gator verify` organizes tests into three levels: Suites, Tests, and Cases:

- A Suite is a file which defines Tests.
- A Test declares a ConstraintTemplate, a Constraint, an ExpansionTemplate (optional), and Cases to test the
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

A Test can also optionally compile an ExpansionTemplate.

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
- `message` is a regular expression used to match the violation message. `message`
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

### Validating Generated Resources with ExpansionTemplates
`gator verify` may be used along with expansion templates to validate generated resources. The expansion template is optionally declared at the test level. If an expansion template is set for a test, gator will attempt to expand each object under the test. The violations for the parent object & its expanded resources will be aggregated.

Example for declaring an expansion template in a Gator Suite:
```yaml
apiVersion: test.gatekeeper.sh/v1alpha1
kind: Suite
tests:
- name: expansion
  template: template.yaml
  constraint: constraint.yaml
  expansion: expansion.yaml
  cases:
  - name: example-expand
    object: deployment.yaml
    assertions:
    - violations: yes
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

## The `gator sync test` subcommand

Certain templates require [replicating data](sync.md) into OPA to enable correct evaluation. These templates can use the annotation `metadata.gatekeeper.sh/requires-sync-data` to indicate which resources need to be synced. The annotation contains a json object representing a list of requirements, each of which contains a list of one or more GVK clauses forming an equivalence set of interchangeable GVKs. Each of these clauses has `groups`, `versions`, and `kinds` fields; any group-version-kind combination within a clause within a requirement should be considered sufficient to satisfy that requirement. For example (comments added for clarity):
```
[
  [ // Requirement 1
    { // Clause 1
      "groups": ["group1", group2"]
      "versions": ["version1", "version2", "version3"]
      "kinds": ["kind1", "kind2"]
    },
    { // Clause 2
      "groups": ["group3", group4"]
      "versions": ["version3", "version4"]
      "kinds": ["kind3", "kind4"]
    }
  ],
  [ // Requirement 2
    { // Clause 1
      "groups": ["group5"]
      "versions": ["version5"]
      "kinds": ["kind5"]
    }
  ]
]
```
This annotation contains two requirements. Requirement 1 contains two clauses. Syncing resources of group1, version3, kind1 (drawn from clause 1) would be sufficient to fulfill Requirement 1. So, too, would syncing resources of group3, version3, kind4 (drawn from clause 2). Syncing resources of group1, version1, and kind3 would not be, however.

Requirement 2 is simpler: it denotes that group5, version5, kind5 must be synced for the policy to work properly.

This template annotation is descriptive, not prescriptive. The prescription of which resources to sync is done in `SyncSet` resources and/or the Gatekeeper `Config` resource. The management of these various requirements can get challenging as the number of templates requiring replicated data increases.

`gator sync test` aims to mitigate this challenge by enabling the user to check that their sync configuration is correct. The user passes in a set of Constraint Templates, GVK Manifest listing GVKs supported by the cluster, SyncSets, and/or a Gatekeeper Config, and the command will determine which requirements enumerated by the Constraint Templates are unfulfilled by the cluster and SyncSet(s)/Config.

### Usage

#### Specifying Inputs

`gator sync test` expects a `--filename` or `--image` flag, or input from stdin. The flags can be used individually, in combination, and/or repeated.

```
gator sync test --filename="template.yaml" –-filename="syncsets/" --filename="manifest.yaml"
```

Or, using an OCI Artifact containing templates as described previously:

```
gator sync test --filename="config.yaml" --image=localhost:5000/gator/template-library:v1
```

The manifest of GVKs supported by the cluster should be passed as a GVKManifest resource (CRD visible under the apis directory in the repo):
```
apiVersion: gvkmanifest.gatekeeper.sh/v1alpha1
kind: GVKManifest
metadata:
  name: gvkmanifest
spec:
  groups:
  - name: "group1"
    versions:
    - name: "v1"
      kinds: ["Kind1", "Kind2"]
    - name: "v2"
      kinds: ["Kind1", "Kind3"]
  - name: "group2"
    versions:
      - name: "v1beta1"
        kinds: ["Kind4", "Kind5"]
```

Optionally, the `--omit-gvk-manifest` flag can be used to skip the requirement of providing a manifest of supported GVKs for the cluster. If this is provided, all GVKs will be assumed to be supported by the cluster. If this assumption is not true, then the given config and templates may cause caching errors or incorrect evaluation on the cluster despite passing this command.

#### Exit Codes

`gator sync test` will return a `0` exit status when the Templates, SyncSets, and
Config are successfully ingested and all requirements are fulfilled.

An error during evaluation, for example a failure to read a file, will result in
a `1` exit status with an error message printed to stderr.

Unfulfilled requirements will generate a `1` exit status as well, and the unfulfilled requirements per template will be printed to stderr, like so:
```
the following requirements were not met:
templatename1:
- extensions/v1beta1:Ingress
- networking.k8s.io/v1beta1:Ingress OR networking.k8s.io/v1:Ingress
templatename2:
- apps/v1:Deployment
templatename3:
- /v1:Service
```



## The `gator bench` subcommand

`gator bench` measures the performance of Gatekeeper policy evaluation. It loads ConstraintTemplates, Constraints, and Kubernetes resources, then repeatedly evaluates the resources against the constraints to gather latency and throughput metrics.

:::note
`gator bench` measures **compute-only** policy evaluation latency, which does not include network round-trip time, TLS overhead, or Kubernetes API server processing. Real-world webhook latency will be higher. Use these metrics for relative comparisons between policy versions, not as absolute production latency predictions.
:::

This command is useful for:
- **Policy developers**: Testing policy performance before deployment
- **Platform teams**: Comparing Rego vs CEL engine performance
- **CI/CD pipelines**: Detecting performance regressions between releases

### Usage

```shell
gator bench --filename=policies/
```

#### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--filename` | `-f` | | File or directory containing ConstraintTemplates, Constraints, and resources. Repeatable. |
| `--image` | `-i` | | OCI image URL containing policies. Repeatable. |
| `--engine` | `-e` | `cel` | Policy engine to benchmark: `rego`, `cel`, or `all` |
| `--iterations` | `-n` | `1000` | Number of benchmark iterations. Use ≥1000 for reliable P99 percentiles. |
| `--warmup` | | `10` | Warmup iterations before measurement |
| `--concurrency` | `-c` | `1` | Number of concurrent goroutines for parallel evaluation |
| `--output` | `-o` | `table` | Output format: `table`, `json`, or `yaml` |
| `--memory` | | `false` | Enable memory profiling (estimates only, not GC-cycle accurate) |
| `--save` | | | Save results to file for future comparison |
| `--compare` | | | Compare against a baseline file |
| `--threshold` | | `10` | Regression threshold percentage (for CI/CD) |
| `--min-threshold` | | `0` | Minimum absolute latency difference to consider (e.g., `100µs`). Useful for fast policies where percentage changes may be noise. |
| `--stats` | | `false` | Gather detailed statistics from constraint framework |

### Examples

#### Basic Benchmark

```shell
gator bench --filename=policies/
```

Output:
```
=== Benchmark Results: Rego Engine ===

Configuration:
  Templates:      5
  Constraints:    10
  Objects:        50
  Iterations:     1000
  Total Reviews:  50000

Timing:
  Setup Duration:  25.00ms
    └─ Client Creation:       0.05ms
    └─ Template Compilation:  20.00ms
    └─ Constraint Loading:    3.00ms
    └─ Data Loading:          1.95ms
  Total Duration:  25.00s
  Throughput:      2000.00 reviews/sec

Latency (per review):
  Min:   200.00µs
  Max:   5.00ms
  Mean:  500.00µs
  P50:   450.00µs
  P95:   1.20ms
  P99:   2.50ms

Results:
  Violations Found:  1500
```

#### Concurrent Benchmarking

Simulate parallel load to test contention behavior:

```shell
gator bench --filename=policies/ --concurrency=4
```

This runs 4 parallel goroutines each executing reviews concurrently.

```
=== Benchmark Results: Rego Engine ===

Configuration:
  Templates:      5
  Constraints:    10
  Objects:        50
  Iterations:     1000
  Concurrency:    4
  Total Reviews:  50000
...
```

#### Compare Rego vs CEL Engines

```shell
gator bench --filename=policies/ --engine=all
```

This runs benchmarks for both engines and displays a comparison table:

```
=== Engine Comparison ===

Metric         Rego        CEL
------         ------      ------
Templates      5           5
Constraints    10          10
Setup Time     25.00ms     15.00ms
Throughput     2000/sec    3500/sec
Mean Latency   500.00µs    285.00µs
P95 Latency    1.20ms      600.00µs
P99 Latency    2.50ms      900.00µs
Violations     150         150

Performance: CEL is 1.75x faster than Rego
```

:::note
Templates without CEL code will be skipped when benchmarking the CEL engine.
A warning will be displayed indicating which templates were skipped.
:::

:::caution
The CEL engine does not support referential constraints. Referential data loading
is skipped entirely when benchmarking with CEL—this is expected behavior, not an error.
If you have policies that rely on referential data (e.g., checking if a namespace exists),
those constraints will not be fully exercised during CEL benchmarks. An informational note
will be displayed indicating that referential data is not supported by the CEL engine.
:::

#### Memory Profiling

```shell
gator bench --filename=policies/ --memory
```

Adds memory statistics to the output:

```
Memory (estimated):
  Allocs/Review:  3000
  Bytes/Review:   150.00 KB
  Total Allocs:   15000000
  Total Bytes:    732.42 MB
```

:::caution
Memory statistics are estimates based on `runtime.MemStats` captured before and after benchmark runs. They do not account for garbage collection cycles that may occur during benchmarking. For production memory analysis, use Go's pprof profiler.
:::

#### Save and Compare Baselines

Save benchmark results as a baseline:

```shell
gator bench --filename=policies/ --memory --save=baseline.json
```

Compare future runs against the baseline:

```shell
gator bench --filename=policies/ --memory --compare=baseline.json
```

Output includes a comparison table:

```
=== Baseline Comparison: Rego Engine ===

Metric         Baseline     Current      Delta   Status
------         --------     -------      -----   ------
P50 Latency    450.00µs     460.00µs     +2.2%   ✓
P95 Latency    1.20ms       1.25ms       +4.2%   ✓
P99 Latency    2.50ms       2.60ms       +4.0%   ✓
Mean Latency   500.00µs     510.00µs     +2.0%   ✓
Throughput     2000/sec     1960/sec     -2.0%   ✓
Allocs/Review  3000         3050         +1.7%   ✓
Bytes/Review   150.00 KB    152.00 KB    +1.3%   ✓

✓ No significant regressions (threshold: 10.0%)
```

For fast policies (< 1ms), small percentage changes may be noise. Use `--min-threshold` to set an absolute minimum difference:

```shell
gator bench --filename=policies/ --compare=baseline.json --threshold=10 --min-threshold=100µs
```

This marks a metric as passing if either:
- The percentage change is within the threshold (10%), OR
- The absolute difference is less than the min-threshold (100µs)

### CI/CD Integration

Use `gator bench` in CI/CD pipelines to detect performance regressions automatically.

#### GitHub Actions Example

```yaml
name: Policy Benchmark

on:
  pull_request:
    paths:
      - 'policies/**'

jobs:
  benchmark:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Download baseline
        uses: actions/download-artifact@v4
        with:
          name: benchmark-baseline
          path: .
        continue-on-error: true  # First run won't have baseline

      - name: Install gator
        run: |
          go install github.com/open-policy-agent/gatekeeper/v3/cmd/gator@latest

      - name: Run benchmark
        run: |
          if [ -f baseline.json ]; then
            # Use min-threshold to avoid flaky failures on fast policies
            gator bench -f policies/ --memory \
              --compare=baseline.json \
              --threshold=10 \
              --min-threshold=100µs
          else
            gator bench -f policies/ --memory --save=baseline.json
          fi

      - name: Upload baseline
        if: github.ref == 'refs/heads/main'
        uses: actions/upload-artifact@v4
        with:
          name: benchmark-baseline
          path: baseline.json
```

:::tip
Use `--min-threshold` in CI to prevent flaky failures. For policies that evaluate in under 1ms, a 10% regression might only be 50µs of noise from system jitter.
:::

#### Exit Codes

| Exit Code | Meaning |
|-----------|---------|
| `0` | Benchmark completed successfully, no regressions detected |
| `1` | Error occurred, or regression threshold exceeded (when using `--compare`) |

When `--compare` is used with `--threshold`, the command exits with code `1` if any metric regresses beyond the threshold. This enables CI/CD pipelines to fail builds that introduce performance regressions.

### Understanding Metrics

| Metric | Description |
|--------|-------------|
| **P50/P95/P99 Latency** | Percentile latencies per review. P99 of 2ms means 99% of reviews complete in ≤2ms. Use ≥1000 iterations for reliable P99. |
| **Mean Latency** | Average time per review |
| **Throughput** | Reviews processed per second |
| **Allocs/Review** | Memory allocations per review (with `--memory`). Estimate only. |
| **Bytes/Review** | Bytes allocated per review (with `--memory`). Estimate only. |
| **Setup Duration** | Time to load templates, constraints, and data |

#### Setup Duration Breakdown

Setup duration includes:
- **Client Creation**: Initializing the constraint client
- **Template Compilation**: Compiling Rego/CEL code in ConstraintTemplates
- **Constraint Loading**: Adding constraints to the client
- **Data Loading**: Loading all Kubernetes resources into the data cache

:::note
Data loading adds all provided resources to the constraint client's cache. This is intentional behavior that matches how Gatekeeper evaluates referential constraints—policies that reference other cluster resources (e.g., checking if a namespace exists) need this cached data available during evaluation.
:::

#### Performance Guidance

- **P99 latency < 100ms** is recommended for production admission webhooks
- **CEL is typically faster than Rego** for equivalent policies
- **High memory allocations** may indicate inefficient policy patterns
- **Setup time** matters for cold starts; consider template compilation cost
- **Concurrency testing** (`--concurrency=N`) reveals contention issues not visible in sequential runs

### Performance Characteristics

The following characteristics are based on architectural differences between policy engines and general benchmarking principles. Actual numbers will vary based on policy complexity, hardware, and workload.

:::tip
These insights were generated using the data gathering scripts in the Gatekeeper repository:
- [`test/gator/bench/scripts/gather-data.sh`](https://github.com/open-policy-agent/gatekeeper/blob/master/test/gator/bench/scripts/gather-data.sh) - Collects benchmark data across different scenarios
- [`test/gator/bench/scripts/analyze-data.sh`](https://github.com/open-policy-agent/gatekeeper/blob/master/test/gator/bench/scripts/analyze-data.sh) - Analyzes and summarizes the collected data

You can run these scripts locally to validate these characteristics on your own hardware.
:::

#### CEL vs Rego

| Characteristic | CEL | Rego |
|----------------|-----|------|
| **Evaluation Speed** | 1.5-3x faster | Baseline |
| **Memory per Review** | 20-30% less | Baseline |
| **Setup/Compilation** | 2-3x slower | Faster |
| **Best For** | Long-running processes | Cold starts |

**Why the difference?**
- CEL compiles to more efficient bytecode, resulting in faster evaluation
- Rego has lighter upfront compilation cost but slower per-evaluation overhead
- For admission webhooks (long-running), CEL's evaluation speed advantage compounds over time

#### Concurrency Scaling

:::note
The `--concurrency` flag simulates parallel policy evaluation similar to how Kubernetes admission webhooks handle concurrent requests. In production, Gatekeeper processes multiple admission requests simultaneously, making concurrent benchmarking essential for realistic performance testing.
:::

- **Linear scaling** up to 4-8 concurrent workers
- **Diminishing returns** beyond CPU core count
- **Increased P99 variance** at high concurrency due to contention
- **Recommendation**: Use 4-8 workers for load testing; match production replica count

```
Concurrency   Typical Efficiency
1             100% (baseline)
2             85-95%
4             70-85%
8             50-70%
16+           <50% (diminishing returns)
```

#### Benchmarking Best Practices

| Practice | Recommendation | Why |
|----------|----------------|-----|
| **Iterations** | ≥1000 | Required for statistically meaningful P99 percentiles |
| **Warmup** | 10 iterations | Go runtime stabilizes quickly; more warmup has minimal impact |
| **Multiple Runs** | 3-5 runs | Expect 2-8% variance between identical runs |
| **P99 vs Mean** | Focus on P99 for SLAs | P99 has higher variance (~8%) than mean (~2%) |
| **CI Thresholds** | Use `--min-threshold` | Prevents flaky failures from natural variance |

#### Interpreting Results

**Healthy patterns:**
- P95/P99 within 2-5x of P50 (consistent performance)
- Memory allocations stable across runs
- Throughput scales with concurrency up to core count

**Warning signs:**
- P99 > 10x P50 (high tail latency, possible GC pressure)
- Memory growing with iteration count (potential leak)
- Throughput decreasing at low concurrency (contention issue)
- Large variance between runs (noisy environment or unstable policy)


## Bundling Policy into OCI Artifacts

It may be useful to bundle policy files into OCI Artifacts for ingestion during
CI/CD workflows. The workflow could perform validation on inbound objects using
`gator test|expand`.

A policy bundle can be composed of any arbitrary file structure, which `gator`
will walk recursively. Any files that do not end in `json|yaml|yml` will be
ignored. `gator` does not enforce any file schema in the artifacts; it only
requires that all files of the support extensions describe valid Kubernetes
resources.

We recommend using the [Oras CLI](https://oras.land/docs/installation) to create OCI
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
