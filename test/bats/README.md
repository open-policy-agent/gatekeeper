# BATS suites

The default `make test-e2e` selection runs the core and VAP-scope suites.
The audit-trigger suite is explicitly selected in the Helm CI lane, including
the custom-namespace variant.

## On-demand audit

Build and deploy the current Helm test image using the usual e2e setup, with
`ENABLE_AUDIT_TRIGGER=true` passed to `make e2e-helm-deploy`. This supplies the
Helm value explicitly; neither the chart defaults nor other test lanes enable
the feature.

From the repository root, against that dedicated test cluster:

```sh
make test-e2e \
  BATS_TESTS_FILE=test/bats/audit_trigger.bats \
  GATEKEEPER_NAMESPACE=gatekeeper-system
```

The suite requires one audit replica, the AuditTrigger CRD/RBAC, and the
standard chart's read-only audit root filesystem. It covers:

- Fresh nonces producing fresh, named violations; unchanged nonces doing no
  additional audit; a new request observing remediation.
- A real scan-input failure preserving the last complete results, followed by
  recovery with a fresh nonce.
- Periodic auditing with the trigger flag omitted, disabled auditing at
  interval zero, and an existing pending request surviving an audit restart.

Do not run this suite concurrently with other tests on the same cluster. It
temporarily changes the audit Deployment arguments/replicas and the singleton
nonce. It creates a separate namespace and a renamed copy of the required-labels
template. Cleanup removes only suite-owned fixtures and restores the original
Deployment settings and pre-existing singleton nonce. Restoring a nonce creates
a new generation; it does not rewind generation or status history.
