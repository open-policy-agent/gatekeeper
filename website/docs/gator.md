---
id: gator
title: The gator CLI
---

The `gator` CLI is a tool for evaluating Gatekeeper ConstraintTemplates and
Constraints in a local environment.

## Installation

To install `gator`, you may either
[download the binary](https://github.com/open-policy-agent/gatekeeper/releases)
relevant to your system or build it directly from source.

To build from source:
```
go get github.com/open-policy-agent/gatekeeper/cmd/gator
```

## The `gator test` subcommand

`gator test` allows users to test a set of Kubernetes objects against a set of
Templates and Constraints.  The command returns violations when found and
communicates success or failure via its exit status.

Note: The `gator verify` command was first called `gator test`.  These names
were changed to better align `gator` with other projects in the
open-policy-agent space.

### Usage

#### Specifying inputs

`gator test` supports inputs through the `--filename` flag and via stdin.  The
two methods of input can be in combination or individually.

The `--filename` flag can specify a single file or a directory.  If a file is
specified, that file must end in one of the following extensions: `.json`,
`.yaml`, `.yml`.  Directories will be walked, and any files of extensions other
than the aforementioned three will be skipped.

For example, to verify a manifest (piped via stdin) against a folder of policies:

```
cat my-manifest.yaml | gator test --filename=template-and-constraints/
```

Or you can specify both as flags:

```
gator test -f=my-manifest.yaml -f=templates-and-constraints/
```

#### Exit Codes

`gator test` will return a `0` exit status when the objects, Templates, and
Constraints are successfully ingested, no errors occur during evaluation, and
no violations are found.

An error during evaluation, for example a failure to read a file, will result
in a `1` exit status with an error message printed to stderr.

Policy violations will generate a `1` exit status as well, but violation
information will be printed to stdout.

##### Enforcement Actions

While violation data will always be returned when an object is found to be
violating a Constraint, the exit status can vary.  A constraint with
`spec.enforcementAction: ""` or `spec.enforcementAction: deny` will produce a
`1` exit code, but other enforcement actions like `dryrun` will not.  This is
meant to make the exit code of `1` consistent with rejection of the object by
Gatekeeper's webhook.  A Constraint set to `warn` would not trigger a rejection
in the webhook, but _would_ produce a violation message.  The same is true for
that constraint when used in `gator test`.

#### Output Formatting

`gator test` supports a `--output` flag that allows the user to specify a
structured data format for the violation data.  This information is printed to
stdout.

The allowed values are `yaml` and `json`, specified like:

```
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

[An example Suite file](https://github.com/open-policy-agent/gatekeeper-library/blob/8765ec11c12a523688ed77485c7a458df84266d6/library/general/allowedrepos/suite.yaml).

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
object is expected to pass or fail validation, and may make assertions about
the returned violations (if any).

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

More examples of working `gator verify` suites are available in the
[gatekeeper-library](https://github.com/open-policy-agent/gatekeeper-library/tree/master/library)
repository.

### Usage

To run a specific suite:
```
gator verify suite.yaml
```

To run all suites in the current directory and all child directories
recursively
```
gator verify ./...
```

To only run tests whose full names contain a match for a regular expression, use
the `run` flag:

```
gator verify path/to/suites/... --run "disallowed"
```

Run `gator verify --help` for more information.

## Gotchas

### Duplicate violation messages

Rego de-duplicates identical violation messages. If you want to be sure that
a test returns multiple violations, use a unique message for each violation.
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
[this commit](https://github.com/open-policy-agent/gatekeeper/commit/b3ed94406583c85f3102c54a32f362d27f76da96).
Continued functionality is not guaranteed.

File paths which include backslashes are not portable, so suites using such
paths will not work as intended on Windows.
