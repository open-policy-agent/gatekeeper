package gktest

import (
	"context"
	"testing"
	"testing/fstest"
)

const templateInteger = `
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

const constraintInteger = `
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
	ctx := context.Background()

	f := &fstest.MapFS{
		"template.yaml": &fstest.MapFile{
			Data: []byte(templateInteger),
		},
		"constraint.yaml": &fstest.MapFile{
			Data: []byte(constraintInteger),
		},
		"allow.yaml": &fstest.MapFile{
			Data: []byte(objectIntegerAllowed),
		},
		"disallow.yaml": &fstest.MapFile{
			Data: []byte(objectIntegerDisallowed),
		},
	}

	runner := Runner{
		FS:        f,
		NewClient: NewOPAClient,
	}

	suite := &Suite{
		Tests: []Test{{
			Template:   "template.yaml",
			Constraint: "constraint.yaml",
			Cases: []Case{{
				Object: "allow.yaml",
			}, {
				Object:     "disallow.yaml",
				Assertions: []Assertion{{Violations: intStrFromInt(1)}},
			}},
		}},
	}

	runner.Run(ctx, Filter{}, "", suite)
}
