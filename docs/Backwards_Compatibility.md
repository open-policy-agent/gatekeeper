# Gatekeeper Backwards Compatibility

## Background
The Gatekeeper project specified a formalization of its backwards compatibility
goals as one of its requirements for reaching its GA milestone. Specifying a
strong stance on backwards compatibility is necessary for establishing a degree
of confidence as to what features users can rely on as they seek to use the
product and avoid churn. This document assumes
[Kubernetes' API compatibility guidelines](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api_changes.md#on-compatibility)
as the default stance and explores exceptions unique to the Gatekeeper project.

## Exclusions
The following areas are out-of-scope for backwards compatibility as Gatekeeper
either has no control over their implementation, does not consider them to be
part of its API, or (in the case of security incidents), must be pragmatic in
weighing risk over convenience.

   * Rego/OPA
      * Gatekeeper does not control the design or implementation of Rego or OPA,
         rather it is a consumer of the project. As such, Gatekeeper will be
         subject to any backwards-incompatible changes to the Rego language.
   * Internal-only resources
      * These resources are intended only for consumption by the Gatekeeper
         project itself and are not intended for consumption by the user.
         These resources are subject to change at any time.
   * Constraint Template Library
      * The library is a consumer of Gatekeeper. As such, it is not bound by
         Gatekeeper's development constraints. That being said, the constraint
         template library should also establish its own philosophy on backwards
         compatibility. The documentation on
         [constraint template versioning](https://docs.google.com/document/d/1vB_2wm60WCVLXoegMrupqwqKAuW6gbwEIxg3vBQj6cs/edit#heading=h.t8fo692xfexq)
         would be a good place to start.
   * Gatekeeper Manifest / Helm template
      * The Gatekeeper manifest and Helm templates are reference deployment
      configurations for Gatekeeper, they are not themselves part of
      Gatekeeper's API. The ability to cleanly upgrade Gatekeeper by re-applying
      the manifest without additional work is subject to breakage. Examples of
      breaking manifest changes include: adding, removing or modifying resource
      kinds, names and contents. Such changes should be highlighted in the
      release notes for the version that creates them. The Gatekeeper binary is
      intended to be written in such a way that it is always possible to upgrade
      from version N to version N+1 without downtime, but the specifics for how
      to do so are out-of-scope for the project.
   * Security Incidents
      * In the event that there is a security incident that is impossible to fix
        without breaking either the schema or the behavior of the Gatekeeper
        API, we would not maintain 100% backwards compatibility at the cost of
        compromising security. We would expect such changes to be rare and
        limited in scope to only what is necessary to address the underlying
        security issue.

## Compatibility of the API
As of GA, Gatekeeper will adopt Kubernetes'
[guidelines on compatibility](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api_changes.md#on-compatibility)
where possible for user-facing resources. The following section discusses places
where Gatekeeper intentionally deviates due to the above exceptions as well as
extra compatibility requirements it is adopting.

### Constraint Templates
Because the constraint template embeds Rego source code as an opaque string
which is then interpreted by OPA under-the-hood, it is possible for the meaning
of that string to change between releases if Rego itself makes a
non-backwards-compatible release. However, the Rego interface that Gatekeeper
controls is expected to remain stable. This means:

   * The expected rule signature for `violation[{"msg": "string", "details": <object>}]`
     should be backwards compatible across releases.
   * The data provided to constraint template source code via the `input` variable
     should be backwards compatible.
   * The storage location of objects in `data.inventory` should be backwards
     compatible, though the backwards compatibility of the schema of the objects
     themselves is not controlled by Gatekeeper and is therefore out-of-scope.

### Constraints
Because constraints are partially defined by constraint templates, it is always
possible for an applied change to a constraint template to break an existing
constraint. Constraints also have a transitive dependency on Rego, so any
backwards-incompatible changes to the language could lead to changes in the
behaviour of the constraint it backs.

However, absent any user-initiated changes to the backing constraint template
or changes to Rego, constraints should have a stable interface. Specifically
this means:

   * The `parameters` schema
   * The `match` schema
      * Note that there is an implicit assumption that the match schema is
        defined for Kubernetes. This will need to be accounted for if we pursue
        multi-target constraints per
        [Expanding the Constraint Framework](https://docs.google.com/document/d/12bmUm2cWuIf3DTENX7yXMfvt_vDKBIHrqJ0on5V5sJo/edit)
   * `enforcementAction`

`enforcementAction` is an interesting beast. It is not an enum, though it looks
like one at first glance. It is an opaque string whose value is given meaning by
the particular enforcement point evaluating the constraint. Its behavior is:

   * the default value is "deny"
   * unknown values should be ignored

The upshot of these differences (particularly "unknown values should be ignored")
is that unlike enums, which have a bounded set of allowed values, the set of
valid enforcement actions is unbounded. This makes it more akin to a
[union set](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#unions).

Current Gatekeeper is deviating slightly from the `enforcementAction` spec by
defaulting to rejecting constraints with an unknown enforcement action (this
behavior can be disabled via the `--disable-enforcementaction-validation` flag).
This should be a non-issue as a missing constraint and an ignored constraint are
functionally equivalent. The reason for rejection is to make typos more visible
to the user. Ultimately, unrecognized values should be returned to the user as a
warning once it is possible for validating webhooks to do so.

### Internal-Only Resources
The following resources are for intended for internal use only and are therefore
not subject to the backwards compatibility design:

   * ConstraintPodStatus
   * ConstraintTemplatePodStatus
   
### Semantic Logging
[Semantic logging](https://docs.google.com/document/d/1ap7AKOupNcR_42s8mkSh5FV9eteXTd4VCqelKst73VY/edit)
should be considered part of our API and subject to backwards compatibility
requirements.

## Feature/API Stage Requirements

Gatekeeper follows the Kubernetes [API versioning conventions](https://kubernetes.io/docs/reference/using-api/#api-versioning) 
and maturity levels (Alpha, Beta, GA) to signal stability and compatibility guarantees. 

### Alpha Stage Requirements

Alpha features and APIs are experimental and may change or be removed without notice. 
They are disabled by default and require explicit opt-in.

**Requirements:**
- Feature must be behind a feature flag or opt-in configuration flag
- API version must be `v1alpha1` or similar alpha designation
- Documentation must:
  - Clearly mark the feature as "Alpha" in all user-facing docs
  - Include warnings about stability and potential for breaking changes
  - Document the opt-in mechanism (flags, configuration)
  - Provide basic usage examples
- Testing must include:
  - Basic unit tests for core functionality
  - At least one integration test demonstrating the feature works
  - Test coverage documenting happy path scenarios
- Must have a documented graduation plan outlining:
  - Required functionality for Beta promotion
  - Known limitations and gaps
  - Anticipated timeline for Beta consideration
- APIs may change in backward-incompatible ways without migration path
- Features may be removed entirely in future releases
- No performance or reliability SLOs required

### Beta Stage Requirements

Beta features are well-tested and enabled by default. The API is considered stable 
enough for production use, though details may still change in compatible ways.

**Requirements:**
- Feature enabled by default (may still have feature flag for disabling)
- API version must be `v1beta1` or similar beta designation
- Multiple API versions may be served, with automatic conversion between them
- Documentation must:
  - Mark feature as "Beta" in user-facing docs
  - Include comprehensive usage guides and examples
- Testing must include:
  - Comprehensive unit test coverage (>80% for new code)
  - Integration tests covering major use cases
  - E2E tests in CI demonstrating real-world scenarios
- Metrics must be exported (may be marked as beta/experimental)
- Performance characteristics must be documented
- Breaking changes require deprecation notices and migration paths

### GA (General Availability) Stage Requirements

GA features are stable, production-ready, and carry strong backward compatibility 
guarantees per this document's compatibility policy.

**Requirements:**
- API version must be `v1` (no alpha/beta designation)
- Documentation must:
  - Remove any Alpha/Beta warnings
  - Be comprehensive and production-ready
  - Provide complete reference documentation
- Testing must include:
  - Complete unit and integration test coverage
  - Soak tests demonstrating stability over time
- Metrics must be stable and documented
- Security review completed and documented
- Backward compatibility guarantees:
  - API schema changes must be backward compatible
  - Default values must not change in breaking ways
  - Behavior changes require deprecation cycle
  - Must support graceful version-to-version upgrades
- Deprecation policy:
  - Deprecated features must be announced in release notes
  - Minimum deprecation period: one minor release cycle
  - Migration path must be documented before deprecation

### Feature Flag Lifecycle

Feature flags follow a structured lifecycle:

1. **Alpha:** Feature disabled by default, requires explicit flag to enable
2. **Beta:** Feature enabled by default, flag allows disabling for rollback
3. **GA:** Flag may be deprecated and eventually removed (after deprecation period)

### Graduation Criteria Checklist

Before promoting a feature, verify:

- [ ] All requirements for target stage are met (see above)
- [ ] API review completed by maintainers
- [ ] Security implications reviewed and addressed
- [ ] Performance impact measured and acceptable
- [ ] Documentation updated to reflect new stage
- [ ] Release notes updated with promotion announcement
- [ ] Test coverage meets stage requirements
- [ ] Metrics instrumentation completed (Beta+)
- [ ] Backward compatibility verified (Beta+)
- [ ] Upgrade/downgrade testing completed (GA)

## Practical Effects

### Project velocity
Adopting this stance on backwards compatibility will necessarily constrain
project velocity wherever its API is concerned. Because we are now attempting to
provide a stable API, changes to our API become either very expensive or
impossible to undo.

This means that any changes to non-alpha resources should be thoroughly vetted
before they are approved.

### Contributions
Our backwards compatibility requirements should be made readily available to
contributors as part of our contributor guide. Reviewers should be on guard
against any changes that may affect backward compatibility.
