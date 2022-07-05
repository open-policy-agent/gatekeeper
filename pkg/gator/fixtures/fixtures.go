package fixtures

const (
	TemplateAlwaysValidate = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: alwaysvalidate
spec:
  crd:
    spec:
      names:
        kind: AlwaysValidate
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8salwaysvalidate
        violation[{"msg": msg}] {
          false
          msg := "should always pass"
        }
`

	TemplateNeverValidate = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: nevervalidate
spec:
  crd:
    spec:
      names:
        kind: NeverValidate
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8snevervalidate
        violation[{"msg": msg}] {
          true
          msg := "never validate"
        }
`

	TemplateNeverValidateTwice = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: nevervalidatetwice
spec:
  crd:
    spec:
      names:
        kind: NeverValidateTwice
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8snevervalidate
        violation[{"msg": msg}] {
          true
          msg := "first message"
        }

        violation[{"msg": msg}] {
          true
          msg := "second message"
        }
`

	TemplateUnsupportedVersion = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta2
metadata:
  name: unsupportedversion
spec:
  crd:
    spec:
      names:
        kind: UnsupportedVersion
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowedtags
        violation[{"msg": msg}] {
          true
          msg := "never validate"
        }
`

	TemplateInvalidYAML = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: alwaysvalidate
  {}: {}
spec:
  crd:
    spec:
      names:
        kind: AlwaysValidate
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowedtags
        violation[{"msg": msg}] {
          true
          msg := "never validate"
        }
`

	TemplateMarshalError = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: alwaysvalidate
spec: [a, b, c]
`

	TemplateCompileError = `
kind: ConstraintTemplate
apiVersion: templates.gatekeeper.sh/v1beta1
metadata:
  name: compileerror
spec:
  crd:
    spec:
      names:
        kind: CompileError
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8sdisallowedtags
        violation[{"msg": msg}] {
          f
          msg := "never validate"
        }
`

	ConstraintAlwaysValidate = `
kind: AlwaysValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: always-pass
`

	ConstraintExcludedNamespace = `
kind: NeverValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: never-validate-namespace
spec:
  match:
    excludedNamespaces: ["excluded"]
`

	ConstraintIncludedNamespace = `
kind: NeverValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: never-validate-namespace
spec:
  match:
    namespaces: ["included"]
`

	ConstraintClusterScope = `
kind: NeverValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: never-validate-namespace
spec:
  match:
    scope: "Cluster"
`

	ConstraintNamespaceSelector = `
kind: NeverValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: never-validate-namespace
spec:
  match:
    namespaceSelector:
      matchLabels:
        bar: qux
`

	ConstraintNeverValidate = `
kind: NeverValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: always-fail
`

	ConstraintNeverValidateTwice = `
kind: NeverValidateTwice
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: always-fail-twice
`

	ConstraintInvalidYAML = `
kind: AlwaysValidate
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: always-pass
  {}: {}
`

	ConstraintWrongTemplate = `
kind: Other
apiVersion: constraints.gatekeeper.sh/v1beta1
metadata:
  name: other
`

	Object = `
kind: Object
apiVersion: group.sh/v1
metadata:
  name: object
`
	ObjectMultiple = `
kind: Object
apiVersion: group.sh/v1
metadata:
  name: object
---
kind: Object
apiVersion: group.sh/v1
metadata:
  name: object-2
`
	ObjectIncluded = `
kind: Object
apiVersion: group.sh/v1
metadata:
  name: object
  namespace: included
`

	ObjectExcluded = `
kind: Object
apiVersion: group.sh/v1
metadata:
  name: object
  namespace: excluded
`

	ObjectNamespaceScope = `
kind: Object
apiVersion: group.sh/v1
metadata:
  name: object
  namespace: foo
`

	ObjectClusterScope = `
kind: Object
apiVersion: group.sh/v1
metadata:
  name: object
`

	ObjectInvalid = `
kind Object
apiVersion: group.sh/v1
metadata:
  name: object`

	ObjectEmpty = ``

	ObjectInvalidInventory = `
kind: Object
metadata:
  name: object
---
kind: Object
apiVersion: group.sh/v1
metadata:
  name: object`

	NamespaceSelected = `
kind: Namespace
apiVersion: /v1
metadata:
  name: foo
  labels:
    bar: qux
`
	NamespaceNotSelected = `
kind: Namespace
apiVersion: /v1
metadata:
  name: foo
  labels:
    bar: bar
`

	TemplateReferential = `
apiVersion: templates.gatekeeper.sh/v1beta1
kind: ConstraintTemplate
metadata:
  name: k8suniqueserviceselector
  annotations:
    description: Requires Services to have unique selectors within a namespace.
spec:
  crd:
    spec:
      names:
        kind: K8sUniqueServiceSelector
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8suniqueserviceselector
        make_apiversion(kind) = apiVersion {
          g := kind.group
          v := kind.version
          g != ""
          apiVersion = sprintf("%v/%v", [g, v])
        }
        make_apiversion(kind) = apiVersion {
          kind.group == ""
          apiVersion = kind.version
        }
        identical(obj, review) {
          obj.metadata.namespace == review.namespace
          obj.metadata.name == review.name
          obj.kind == review.kind.kind
          obj.apiVersion == make_apiversion(review.kind)
        }
        flatten_selector(obj) = flattened {
          selectors := [s | s = concat(":", [key, val]); val = obj.spec.selector[key]]
          flattened := concat(",", sort(selectors))
        }
        violation[{"msg": msg}] {
          input.review.kind.kind == "Service"
          input.review.kind.version == "v1"
          input.review.kind.group == ""
          input_selector := flatten_selector(input.review.object)
          other := data.inventory.namespace[namespace][_]["Service"][name]
          not identical(other, input.review)
          other_selector := flatten_selector(other)
          input_selector == other_selector
          msg := sprintf("same selector as service <%v> in namespace <%v>", [name, namespace])
        }
`

	ConstraintReferential = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sUniqueServiceSelector
metadata:
  name: unique-service-selector
  labels:
    owner: admin.agilebank.demo
`

	ObjectReferentialInventory = `
apiVersion: v1
kind: Service
metadata:
  name: gatekeeper-test-service-example
  namespace: default
spec:
  ports:
    - port: 443
  selector:
    key: value
`

	ObjectReferentialAllow = `
apiVersion: v1
kind: Service
metadata:
  name: gatekeeper-test-service-allowed
  namespace: default
spec:
  ports:
    - port: 443
  selector:
    key: other-value
`

	ObjectReferentialDeny = `
apiVersion: v1
kind: Service
metadata:
  name: gatekeeper-test-service-disallowed
  namespace: default
spec:
  ports:
    - port: 443
  selector:
    key: value
`

	TemplateRequiredLabel = `
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
      validation:
        # Schema for the parameters field
        openAPIV3Schema:
          type: object
          properties:
            labels:
              type: array
              items:
                type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels

        violation[{"msg": msg, "details": {"missing_labels": missing}}] {
          provided := {label | input.review.object.metadata.labels[label]}
          required := {label | label := input.parameters.labels[_]}
          missing := required - provided
          count(missing) > 0
          ns := [n | data.inventory.cluster.v1.Namespace[n]]
          msg := sprintf("I can grab namespaces... %v ... and dump the inventory... %v", [ns, data.inventory])
        }
`

	ConstraintRequireLabelInvalid = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: required-labels
spec:
  parameters:
    labels: "abc"
`

	ConstraintRequireLabelValid = `
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: required-labels
spec:
  parameters:
    labels: ["abc"]
`
)
