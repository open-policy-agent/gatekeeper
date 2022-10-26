package gator

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

const templateV1Beta1Integer = `
apiVersion: templates.gatekeeper.sh/v1beta1
kind: ConstraintTemplate
metadata:
  name: k8sreplicalimits
  annotations:
    description: Requires a number of replicas to be set for a deployment between a min and max value.
spec:
  crd:
    spec:
      names:
        kind: K8sReplicaLimits
      validation:
        # Schema for the parameters field
        openAPIV3Schema:
          type: object
          properties:
            ranges:
              type: array
              items:
                type: object
                properties:
                  min_replicas:
                    type: integer
                  max_replicas:
                    type: integer
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sreplicalimits
        deployment_name = input.review.object.metadata.name
        violation[{"msg": msg}] {
          spec := input.review.object.spec
          not input_replica_limit(spec)
          msg := sprintf("The provided number of replicas is not allowed for deployment: %v. Allowed ranges: %v", [deployment_name, input.parameters])
        }
        input_replica_limit(spec) {
          provided := input.review.object.spec.replicas
          count(input.parameters.ranges) > 0
          range := input.parameters.ranges[_]
          value_within_range(range, provided)
        }
        value_within_range(range, value) {
          range.min_replicas <= value
          range.max_replicas >= value
        }
`

const templateV1Integer = `
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8sreplicalimits
  annotations:
    description: Requires a number of replicas to be set for a deployment between a min and max value.
spec:
  crd:
    spec:
      names:
        kind: K8sReplicaLimits
      validation:
        # Schema for the parameters field
        openAPIV3Schema:
          type: object
          properties:
            ranges:
              type: array
              items:
                type: object
                properties:
                  min_replicas:
                    type: integer
                  max_replicas:
                    type: integer
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sreplicalimits
        deployment_name = input.review.object.metadata.name
        violation[{"msg": msg}] {
          spec := input.review.object.spec
          not input_replica_limit(spec)
          msg := sprintf("The provided number of replicas is not allowed for deployment: %v. Allowed ranges: %v", [deployment_name, input.parameters])
        }
        input_replica_limit(spec) {
          provided := input.review.object.spec.replicas
          count(input.parameters.ranges) > 0
          range := input.parameters.ranges[_]
          value_within_range(range, provided)
        }
        value_within_range(range, value) {
          range.min_replicas <= value
          range.max_replicas >= value
        }
`

const templateV1Beta1IntegerNonStructural = `
apiVersion: templates.gatekeeper.sh/v1beta1
kind: ConstraintTemplate
metadata:
  name: k8sreplicalimits
  annotations:
    description: Requires a number of replicas to be set for a deployment between a min and max value.
spec:
  crd:
    spec:
      names:
        kind: K8sReplicaLimits
      validation:
        # Schema for the parameters field
        openAPIV3Schema:
          properties:
            ranges:
              type: array
              items:
                properties:
                  min_replicas: {}
                  max_replicas: {}
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sreplicalimits
        deployment_name = input.review.object.metadata.name
        violation[{"msg": msg}] {
          spec := input.review.object.spec
          not input_replica_limit(spec)
          msg := sprintf("The provided number of replicas is not allowed for deployment: %v. Allowed ranges: %v", [deployment_name, input.parameters])
        }
        input_replica_limit(spec) {
          provided := input.review.object.spec.replicas
          count(input.parameters.ranges) > 0
          range := input.parameters.ranges[_]
          value_within_range(range, provided)
        }
        value_within_range(range, value) {
          range.min_replicas <= value
          range.max_replicas >= value
        }
`

const constraintV1Beta1Integer = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sReplicaLimits
metadata:
  name: replica-limits
spec:
  match:
    kinds:
      - apiGroups: ["apps"]
        kinds: ["Deployment"]
  parameters:
    ranges:
    - min_replicas: 3
      max_replicas: 50
`

const constraintV1Integer = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sReplicaLimits
metadata:
  name: replica-limits
spec:
  match:
    kinds:
      - apiGroups: ["apps"]
        kinds: ["Deployment"]
  parameters:
    ranges:
    - min_replicas: 3
      max_replicas: 50
`

const objectIntegerAllowed = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: allowed-deployment
spec:
  selector:
    matchLabels:
      app: nginx
  replicas: 3
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
`

const objectIntegerDisallowed = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: disallowed-deployment
spec:
  selector:
    matchLabels:
      app: nginx
  replicas: 100
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
`

func TestRunner_Run_Integer(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		template   string
		constraint string
	}{
		{
			name:       "structural v1beta1 templates",
			template:   templateV1Beta1Integer,
			constraint: constraintV1Beta1Integer,
		},
		{
			name:       "non-structural v1beta1 template",
			template:   templateV1Beta1IntegerNonStructural,
			constraint: constraintV1Beta1Integer,
		},
		{
			name:       "v1 template",
			template:   templateV1Integer,
			constraint: constraintV1Integer,
		},
	}

	for _, tc := range testCases {
		// Required for parallel tests.
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			f := &fstest.MapFS{
				"template.yaml": &fstest.MapFile{
					Data: []byte(tc.template),
				},
				"constraint.yaml": &fstest.MapFile{
					Data: []byte(tc.constraint),
				},
				"allow.yaml": &fstest.MapFile{
					Data: []byte(objectIntegerAllowed),
				},
				"disallow.yaml": &fstest.MapFile{
					Data: []byte(objectIntegerDisallowed),
				},
			}

			runner, err := NewRunner(f, NewOPAClient, false)
			if err != nil {
				t.Fatal(err)
			}

			suite := &Suite{
				Tests: []Test{{
					Template:   "template.yaml",
					Constraint: "constraint.yaml",
					Cases: []*Case{{
						Object:     "allow.yaml",
						Assertions: []Assertion{{Violations: intStrFromInt(0)}},
					}, {
						Object:     "disallow.yaml",
						Assertions: []Assertion{{Violations: intStrFromInt(1)}},
					}},
				}},
			}

			result := runner.Run(ctx, &nilFilter{}, suite)
			if !result.IsFailure() {
				return
			}

			sb := strings.Builder{}
			err = PrinterGo{}.PrintSuite(&sb, &result, true)
			if err != nil {
				t.Fatal(err)
			}

			t.Log(sb.String())
			t.Fatal("got failure but want success")
		})
	}
}
