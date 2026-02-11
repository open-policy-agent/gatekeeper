#!/usr/bin/env bats
#
# E2E tests for gator policy command
#
# These tests require:
# - A running Kubernetes cluster with Gatekeeper installed
# - The gator binary built and available in PATH or ./bin/gator
#

GATOR="${GATOR:-./bin/gator}"
# Temp location for testing
CATALOG_URL="https://raw.githubusercontent.com/sozercan/gatekeeper-library/refs/heads/bundles/catalog.yaml"

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
    kubectl delete constrainttemplate k8srequiredlabels 2>/dev/null || true
    kubectl delete constrainttemplate k8scontainerlimits 2>/dev/null || true
    kubectl delete constrainttemplate k8spspprivilegedcontainer 2>/dev/null || true
    kubectl delete constrainttemplate k8sdisallowedtags 2>/dev/null || true
}

@test "gator policy update downloads catalog" {
    run ${GATOR} policy update
    [ "$status" -eq 0 ]
    [[ "$output" =~ "gatekeeper-library" ]] || [[ "$output" =~ "Updated" ]] || [[ "$output" =~ "Fetching" ]]
}

@test "gator policy search finds policies" {
    # Update catalog first
    ${GATOR} policy update

    run ${GATOR} policy search labels
    [ "$status" -eq 0 ]
    [[ "$output" =~ "k8srequiredlabels" ]]
}

@test "gator policy search with category filter" {
    ${GATOR} policy update

    run ${GATOR} policy search privileged --category=pod-security
    [ "$status" -eq 0 ]
    [[ "$output" =~ "k8spspprivilegedcontainer" ]] || [[ "$output" =~ "No policies found" ]]
}

@test "gator policy search with json output" {
    ${GATOR} policy update

    run ${GATOR} policy search required --output=json
    [ "$status" -eq 0 ]
    # Should be valid JSON
    echo "$output" | jq . > /dev/null
}

@test "gator policy install single policy" {
    ${GATOR} policy update

    run ${GATOR} policy install k8srequiredlabels
    [ "$status" -eq 0 ]
    [[ "$output" =~ "installed" ]] || [[ "$output" =~ "k8srequiredlabels" ]]

    # Verify it's in the cluster
    run kubectl get constrainttemplate k8srequiredlabels
    [ "$status" -eq 0 ]

    # Verify labels
    run kubectl get constrainttemplate k8srequiredlabels -o jsonpath='{.metadata.labels.gatekeeper\.sh/managed-by}'
    [ "$status" -eq 0 ]
    [ "$output" = "gator" ]
}

@test "gator policy install with dry-run" {
    ${GATOR} policy update

    # First make sure it's not installed
    kubectl delete constrainttemplate k8scontainerlimits 2>/dev/null || true

    run ${GATOR} policy install k8scontainerlimits --dry-run
    [ "$status" -eq 0 ]
    [[ "$output" =~ "dry-run" ]] || [[ "$output" =~ "Dry run" ]] || [[ "$output" =~ "Would" ]] || [[ "$output" =~ "k8scontainerlimits" ]]

    # Verify it was NOT actually installed
    run kubectl get constrainttemplate k8scontainerlimits
    [ "$status" -ne 0 ]
}

@test "gator policy list shows installed policies" {
    ${GATOR} policy update

    # Install a policy first
    ${GATOR} policy install k8srequiredlabels

    run ${GATOR} policy list
    [ "$status" -eq 0 ]
    [[ "$output" =~ "k8srequiredlabels" ]]
}

@test "gator policy list with json output" {
    ${GATOR} policy update
    ${GATOR} policy install k8srequiredlabels

    run ${GATOR} policy list --output=json
    [ "$status" -eq 0 ]
    # Should be valid JSON
    echo "$output" | jq . > /dev/null
    # Should contain the policy
    [[ "$output" =~ "k8srequiredlabels" ]]
}

@test "gator policy uninstall removes policy" {
    ${GATOR} policy update

    # Install first
    ${GATOR} policy install k8srequiredlabels

    # Verify it exists
    kubectl get constrainttemplate k8srequiredlabels

    # Uninstall
    run ${GATOR} policy uninstall k8srequiredlabels
    [ "$status" -eq 0 ]
    [[ "$output" =~ "uninstall" ]] || [[ "$output" =~ "removed" ]] || [[ "$output" =~ "k8srequiredlabels" ]]

    # Verify it's gone
    run kubectl get constrainttemplate k8srequiredlabels
    [ "$status" -ne 0 ]
}

@test "gator policy install bundle" {
    ${GATOR} policy update

    run ${GATOR} policy install --bundle pod-security-baseline
    [ "$status" -eq 0 ]

    # Verify some policies from bundle are installed
    run kubectl get constrainttemplate k8spspprivilegedcontainer
    [ "$status" -eq 0 ]
}

@test "gator policy upgrade --all" {
    ${GATOR} policy update

    # Install a policy
    ${GATOR} policy install k8srequiredlabels

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
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package test
        violation[{"msg": msg}] {
          msg := "test"
        }
EOF

    # Try to install - should fail with conflict
    run ${GATOR} policy install k8srequiredlabels
    [ "$status" -ne 0 ]
    [[ "$output" =~ "not managed by gator" ]] || [[ "$output" =~ "conflict" ]] || [[ "$output" =~ "Conflict" ]]

    # Clean up
    kubectl delete constrainttemplate k8srequiredlabels
}

@test "gator policy handles missing catalog gracefully" {
    # Use non-existent catalog
    export GATOR_CATALOG_URL="file:///nonexistent/catalog.yaml"

    run ${GATOR} policy update
    [ "$status" -ne 0 ]

    # Restore
    export GATOR_CATALOG_URL="${CATALOG_URL}"
}

@test "gator policy generate-catalog with invalid path fails" {
    run ${GATOR} policy generate-catalog --library-path=/nonexistent/path
    [ "$status" -ne 0 ]
    [[ "$output" =~ "does not exist" ]] || [[ "$output" =~ "not found" ]] || [[ "$output" =~ "library" ]]
}

@test "gator policy generate-catalog from library" {
    if [[ "${GATEKEEPER_LIBRARY_PATH:-}" == "" ]]; then
        skip "GATEKEEPER_LIBRARY_PATH not set"
    fi

    run ${GATOR} policy generate-catalog \
        --library-path="${GATEKEEPER_LIBRARY_PATH}" \
        --output=/tmp/test-catalog.yaml \
        --validate
    [ "$status" -eq 0 ]
    [[ "$output" =~ "policies" ]] || [[ "$output" =~ "Catalog" ]]

    # Verify file was created
    [ -f /tmp/test-catalog.yaml ]

    # Verify it's valid YAML with expected fields
    grep -q "apiVersion" /tmp/test-catalog.yaml
    grep -q "PolicyCatalog" /tmp/test-catalog.yaml

    rm -f /tmp/test-catalog.yaml
}

@test "gator policy generate-catalog with base-url converts paths" {
    if [[ "${GATEKEEPER_LIBRARY_PATH:-}" == "" ]]; then
        skip "GATEKEEPER_LIBRARY_PATH not set"
    fi

    run ${GATOR} policy generate-catalog \
        --library-path="${GATEKEEPER_LIBRARY_PATH}" \
        --output=/tmp/test-catalog-url.yaml \
        --base-url=https://raw.githubusercontent.com/open-policy-agent/gatekeeper-library/master \
        --validate
    [ "$status" -eq 0 ]

    # Verify paths are converted to URLs
    grep -q "https://raw.githubusercontent.com" /tmp/test-catalog-url.yaml

    rm -f /tmp/test-catalog-url.yaml
}

@test "gator policy generate-catalog with bundles file" {
    if [[ "${GATEKEEPER_LIBRARY_PATH:-}" == "" ]]; then
        skip "GATEKEEPER_LIBRARY_PATH not set"
    fi

    # Create a temporary bundles file
    cat > /tmp/test-bundles.yaml <<EOF
bundles:
  - name: test-bundle
    description: Test bundle for E2E
    policies:
      - k8srequiredlabels
EOF

    run ${GATOR} policy generate-catalog \
        --library-path="${GATEKEEPER_LIBRARY_PATH}" \
        --output=/tmp/test-catalog-bundles.yaml \
        --bundles=/tmp/test-bundles.yaml \
        --validate
    [ "$status" -eq 0 ]
    [[ "$output" =~ "bundle" ]] || [[ "$output" =~ "1 bundles" ]]

    rm -f /tmp/test-catalog-bundles.yaml /tmp/test-bundles.yaml
}

@test "gator policy generate-catalog with custom name and version" {
    if [[ "${GATEKEEPER_LIBRARY_PATH:-}" == "" ]]; then
        skip "GATEKEEPER_LIBRARY_PATH not set"
    fi

    run ${GATOR} policy generate-catalog \
        --library-path="${GATEKEEPER_LIBRARY_PATH}" \
        --output=/tmp/test-catalog-custom.yaml \
        --name=my-custom-catalog \
        --version=v2.5.0 \
        --validate
    [ "$status" -eq 0 ]

    # Verify custom metadata
    grep -q "my-custom-catalog" /tmp/test-catalog-custom.yaml
    grep -q "v2.5.0" /tmp/test-catalog-custom.yaml

    rm -f /tmp/test-catalog-custom.yaml
}

