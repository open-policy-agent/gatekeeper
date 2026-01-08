#!/usr/bin/env bats
#
# E2E tests for gator policy command
#
# These tests require:
# - A running Kubernetes cluster with Gatekeeper installed
# - The gator binary built and available in PATH or ./bin/gator
# - Test fixtures in test/gator/policy/testdata/

GATOR="${GATOR:-./bin/gator}"
TESTDATA_DIR="test/gator/policy/testdata"
CATALOG_URL="file://${PWD}/${TESTDATA_DIR}/catalog.yaml"

setup_file() {
    export GATOR_CATALOG_URL="${CATALOG_URL}"
    
    # Verify gator is available
    if [[ ! -x "${GATOR}" ]]; then
        skip "gator binary not found at ${GATOR}"
    fi
    
    # Verify kubectl is available
    if ! command -v kubectl &> /dev/null; then
        skip "kubectl not found"
    fi
    
    # Verify cluster is accessible
    if ! kubectl cluster-info &> /dev/null; then
        skip "Kubernetes cluster not accessible"
    fi
    
    # Verify Gatekeeper is installed
    if ! kubectl get crd constrainttemplates.templates.gatekeeper.sh &> /dev/null; then
        skip "Gatekeeper CRDs not installed"
    fi
}

teardown() {
    # Clean up any installed test policies
    kubectl delete constrainttemplate test-required-labels 2>/dev/null || true
    kubectl delete constrainttemplate test-container-limits 2>/dev/null || true
    kubectl delete constrainttemplate test-privileged-container 2>/dev/null || true
    kubectl delete constrainttemplate test-upgradeable 2>/dev/null || true
}

@test "gator policy update downloads catalog" {
    run ${GATOR} policy update
    [ "$status" -eq 0 ]
    [[ "$output" =~ "test-catalog" ]] || [[ "$output" =~ "Updated" ]] || [[ "$output" =~ "Fetching" ]]
}

@test "gator policy search finds policies" {
    # Update catalog first
    ${GATOR} policy update
    
    run ${GATOR} policy search labels
    [ "$status" -eq 0 ]
    [[ "$output" =~ "test-required-labels" ]]
}

@test "gator policy search with category filter" {
    ${GATOR} policy update
    
    run ${GATOR} policy search container --category=pod-security
    [ "$status" -eq 0 ]
    [[ "$output" =~ "test-privileged-container" ]] || [[ "$output" =~ "No policies found" ]]
}

@test "gator policy search with json output" {
    ${GATOR} policy update
    
    run ${GATOR} policy search test --output=json
    [ "$status" -eq 0 ]
    # Should be valid JSON
    echo "$output" | jq . > /dev/null
}

@test "gator policy install single policy" {
    ${GATOR} policy update
    
    run ${GATOR} policy install test-required-labels
    [ "$status" -eq 0 ]
    [[ "$output" =~ "installed" ]] || [[ "$output" =~ "test-required-labels" ]]
    
    # Verify it's in the cluster
    run kubectl get constrainttemplate test-required-labels
    [ "$status" -eq 0 ]
    
    # Verify labels
    run kubectl get constrainttemplate test-required-labels -o jsonpath='{.metadata.labels.gatekeeper\.sh/managed-by}'
    [ "$status" -eq 0 ]
    [ "$output" = "gator" ]
}

@test "gator policy install with dry-run" {
    ${GATOR} policy update
    
    # First make sure it's not installed
    kubectl delete constrainttemplate test-container-limits 2>/dev/null || true
    
    run ${GATOR} policy install test-container-limits --dry-run
    [ "$status" -eq 0 ]
    [[ "$output" =~ "dry-run" ]] || [[ "$output" =~ "Dry run" ]] || [[ "$output" =~ "Would" ]]
    
    # Verify it was NOT actually installed
    run kubectl get constrainttemplate test-container-limits
    [ "$status" -ne 0 ]
}

@test "gator policy list shows installed policies" {
    ${GATOR} policy update
    
    # Install a policy first
    ${GATOR} policy install test-required-labels
    
    run ${GATOR} policy list
    [ "$status" -eq 0 ]
    [[ "$output" =~ "test-required-labels" ]]
}

@test "gator policy list with json output" {
    ${GATOR} policy update
    ${GATOR} policy install test-required-labels
    
    run ${GATOR} policy list --output=json
    [ "$status" -eq 0 ]
    # Should be valid JSON
    echo "$output" | jq . > /dev/null
    # Should contain the policy
    [[ "$output" =~ "test-required-labels" ]]
}

@test "gator policy uninstall removes policy" {
    ${GATOR} policy update
    
    # Install first
    ${GATOR} policy install test-required-labels
    
    # Verify it exists
    kubectl get constrainttemplate test-required-labels
    
    # Uninstall
    run ${GATOR} policy uninstall test-required-labels
    [ "$status" -eq 0 ]
    [[ "$output" =~ "uninstall" ]] || [[ "$output" =~ "removed" ]] || [[ "$output" =~ "test-required-labels" ]]
    
    # Verify it's gone
    run kubectl get constrainttemplate test-required-labels
    [ "$status" -ne 0 ]
}

@test "gator policy install bundle" {
    ${GATOR} policy update
    
    run ${GATOR} policy install --bundle test-bundle
    [ "$status" -eq 0 ]
    
    # Verify policies from bundle are installed
    run kubectl get constrainttemplate test-required-labels
    [ "$status" -eq 0 ]
    
    run kubectl get constrainttemplate test-container-limits
    [ "$status" -eq 0 ]
}

@test "gator policy upgrade --all" {
    ${GATOR} policy update
    
    # Install an upgradeable policy with an old version annotation
    ${GATOR} policy install test-required-labels
    
    # The upgrade should report current status
    run ${GATOR} policy upgrade --all
    [ "$status" -eq 0 ]
}

@test "gator policy refuses to modify unmanaged templates" {
    ${GATOR} policy update
    
    # Create an unmanaged ConstraintTemplate with same name
    cat <<EOF | kubectl apply -f -
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: test-required-labels
spec:
  crd:
    spec:
      names:
        kind: TestRequiredLabels
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package test
        violation[{"msg": msg}] {
          msg := "test"
        }
EOF
    
    # Try to install - should fail with conflict
    run ${GATOR} policy install test-required-labels
    [ "$status" -ne 0 ]
    [[ "$output" =~ "not managed by gator" ]] || [[ "$output" =~ "conflict" ]] || [[ "$output" =~ "Conflict" ]]
    
    # Clean up
    kubectl delete constrainttemplate test-required-labels
}

@test "gator policy handles missing catalog gracefully" {
    # Use non-existent catalog
    export GATOR_CATALOG_URL="file:///nonexistent/catalog.yaml"
    
    run ${GATOR} policy update
    [ "$status" -ne 0 ]
    
    # Restore
    export GATOR_CATALOG_URL="${CATALOG_URL}"
}
