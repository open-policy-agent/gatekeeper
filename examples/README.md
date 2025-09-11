# Gatekeeper Mutation Examples

This directory contains examples demonstrating Gatekeeper mutation capabilities, including the new `operations` field for granular operation control.

## Operations Field Overview

The `operations` field in the `applyTo` section allows you to specify which Kubernetes admission operations should trigger mutations:

- **`CREATE`** - Apply mutation when resources are created
- **`UPDATE`** - Apply mutation when resources are updated  
- **`DELETE`** - Apply mutation when resources are deleted (advanced use cases)

### Default Behavior (Backward Compatibility)

If no `operations` field is specified, mutations default to `["CREATE", "UPDATE"]` to maintain compatibility with existing deployments.

## Example Files

### assign-with-operations.yaml

Demonstrates various patterns for using the `operations` field:

1. **CREATE-only mutation** - Solves immutable field issues during updates
2. **Explicit CREATE+UPDATE** - Shows how to explicitly specify default behavior
3. **Legacy compatibility** - Existing mutations without operations field
4. **DELETE operations** - Advanced use cases (use with caution)
5. **All operations** - Comprehensive mutation coverage

## Common Use Cases

### Solving Immutable Field Issues

Many Kubernetes resources have immutable fields that cannot be changed after creation. Setting environment variables on Pods is a common example:

```yaml
operations: ["CREATE"]  # Only mutate during creation
location: "spec.containers[name:*].env[name:MY_VAR].value"
```

### Default Value Setting

For fields that should have defaults on creation but can be changed later:

```yaml
operations: ["CREATE"]
location: "spec.containers[name:*].imagePullPolicy"
parameters:
  assign:
    value: "IfNotPresent"
```

### Dynamic Updates

For mutations that should apply whenever resources are modified:

```yaml
operations: ["UPDATE"]
location: "metadata.labels.last-updated"
parameters:
  assign:
    fromMetadata:
      field: "timestamp"
```

### Legacy Compatibility

Existing mutations continue to work without changes:

```yaml
# No operations field = ["CREATE", "UPDATE"] behavior
applyTo:
- groups: [""]
  kinds: ["Pod"]
  versions: ["v1"]
```

## Migration Guide

### From Existing Mutations

1. **No action required** - Existing mutations work unchanged
2. **Gradual adoption** - Add operations field to new mutations as needed
3. **Optional updates** - Update existing mutations only if you need different operation behavior

### Best Practices

1. **Start with CREATE-only** for immutable fields
2. **Use explicit operations** for clarity in new mutations
3. **Avoid DELETE operations** unless you have specific cleanup requirements
4. **Test thoroughly** when changing operation behavior on existing mutations

## Related Documentation

- [Mutation Documentation](https://open-policy-agent.github.io/gatekeeper/website/docs/mutation/)
- [Gatekeeper Installation](https://open-policy-agent.github.io/gatekeeper/website/docs/install/)
- [Policy Library](https://open-policy-agent.github.io/gatekeeper-library/website/)
