kubectl apply -f - <<EOF
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: testownerref
spec:
  crd:
    spec:
      names:
        kind: TestOwnerRef
      validation:
        openAPIV3Schema:
          type: object
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package testownerref
        violation[{"msg": "test"}] {
          true
        }
EOF

sleep 1

err=$(kubectl get ConstraintTemplate testownerref -o jsonpath='{.status.byPod[].errors}')

if [[ -n "$err" ]]; then
  echo "ConstraintTemplate testownerref is in error state: $err"
  exit 1
fi