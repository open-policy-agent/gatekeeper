package target

// DO NOT MODIFY THIS FILE DIRECTLY!
// This file is generated from ./pkg/mutation/match/match_types.go via "make manifests".

const matchYAML = `
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.14.0
  name: matchcrd.match.gatekeeper.sh
spec:
  group: match.gatekeeper.sh
  names:
    kind: DummyCRD
    listKind: DummyCRDList
    plural: matchcrd
    singular: dummycrd
  scope: Namespaced
  versions:
  - name: match
    schema:
      openAPIV3Schema:
        description: |-
          DummyCRD is a "dummy" CRD to hold the Match object, which we ultimately
          need to generate JSONSchemaProps. The TypeMeta and ObjectMeta fields are
          required for controller-gen to generate the CRD.
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          embeddedMatch:
            description: Match selects which objects are in scope.
            properties:
              excludedNamespaces:
                description: |-
                  ExcludedNamespaces is a list of namespace names. If defined, a
                  constraint only applies to resources not in a listed namespace.
                  ExcludedNamespaces also supports a prefix or suffix based glob.  For example,
                  `+"`"+`excludedNamespaces: [kube-*]`+"`"+` matches both `+"`"+`kube-system`+"`"+` and
                  `+"`"+`kube-public`+"`"+`, and `+"`"+`excludedNamespaces: [*-system]`+"`"+` matches both `+"`"+`kube-system`+"`"+` and
                  `+"`"+`gatekeeper-system`+"`"+`.
                items:
                  description: |-
                    A string that supports globbing at its front and end. Ex: "kube-*" will match "kube-system" or
                    "kube-public", "*-system" will match "kube-system" or "gatekeeper-system", "*system*" will
                    match "system-kube" or "kube-system".  The asterisk is required for wildcard matching.
                  pattern: ^\*?[-:a-z0-9]*\*?$
                  type: string
                type: array
              kinds:
                items:
                  description: |-
                    Kinds accepts a list of objects with apiGroups and kinds fields
                    that list the groups/kinds of objects to which the mutation will apply.
                    If multiple groups/kinds objects are specified,
                    only one match is needed for the resource to be in scope.
                  properties:
                    apiGroups:
                      description: |-
                        APIGroups is the API groups the resources belong to. '*' is all groups.
                        If '*' is present, the length of the slice must be one.
                        Required.
                      items:
                        type: string
                      type: array
                    kinds:
                      items:
                        type: string
                      type: array
                  type: object
                type: array
              labelSelector:
                description: |-
                  LabelSelector is the combination of two optional fields: `+"`"+`matchLabels`+"`"+`
                  and `+"`"+`matchExpressions`+"`"+`.  These two fields provide different methods of
                  selecting or excluding k8s objects based on the label keys and values
                  included in object metadata.  All selection expressions from both
                  sections are ANDed to determine if an object meets the cumulative
                  requirements of the selector.
                properties:
                  matchExpressions:
                    description: matchExpressions is a list of label selector requirements.
                      The requirements are ANDed.
                    items:
                      description: |-
                        A label selector requirement is a selector that contains values, a key, and an operator that
                        relates the key and values.
                      properties:
                        key:
                          description: key is the label key that the selector applies
                            to.
                          type: string
                        operator:
                          description: |-
                            operator represents a key's relationship to a set of values.
                            Valid operators are In, NotIn, Exists and DoesNotExist.
                          type: string
                        values:
                          description: |-
                            values is an array of string values. If the operator is In or NotIn,
                            the values array must be non-empty. If the operator is Exists or DoesNotExist,
                            the values array must be empty. This array is replaced during a strategic
                            merge patch.
                          items:
                            type: string
                          type: array
                          x-kubernetes-list-type: atomic
                      required:
                      - key
                      - operator
                      type: object
                    type: array
                    x-kubernetes-list-type: atomic
                  matchLabels:
                    additionalProperties:
                      type: string
                    description: |-
                      matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                      map is equivalent to an element of matchExpressions, whose key field is "key", the
                      operator is "In", and the values array contains only "value". The requirements are ANDed.
                    type: object
                type: object
                x-kubernetes-map-type: atomic
              name:
                description: |-
                  Name is the name of an object.  If defined, it will match against objects with the specified
                  name.  Name also supports a prefix or suffix glob.  For example, `+"`"+`name: pod-*`+"`"+` would match
                  both `+"`"+`pod-a`+"`"+` and `+"`"+`pod-b`+"`"+`, and `+"`"+`name: *-pod`+"`"+` would match both `+"`"+`a-pod`+"`"+` and `+"`"+`b-pod`+"`"+`.
                pattern: ^\*?[-:a-z0-9]*\*?$
                type: string
              namespaceSelector:
                description: |-
                  NamespaceSelector is a label selector against an object's containing
                  namespace or the object itself, if the object is a namespace.
                properties:
                  matchExpressions:
                    description: matchExpressions is a list of label selector requirements.
                      The requirements are ANDed.
                    items:
                      description: |-
                        A label selector requirement is a selector that contains values, a key, and an operator that
                        relates the key and values.
                      properties:
                        key:
                          description: key is the label key that the selector applies
                            to.
                          type: string
                        operator:
                          description: |-
                            operator represents a key's relationship to a set of values.
                            Valid operators are In, NotIn, Exists and DoesNotExist.
                          type: string
                        values:
                          description: |-
                            values is an array of string values. If the operator is In or NotIn,
                            the values array must be non-empty. If the operator is Exists or DoesNotExist,
                            the values array must be empty. This array is replaced during a strategic
                            merge patch.
                          items:
                            type: string
                          type: array
                          x-kubernetes-list-type: atomic
                      required:
                      - key
                      - operator
                      type: object
                    type: array
                    x-kubernetes-list-type: atomic
                  matchLabels:
                    additionalProperties:
                      type: string
                    description: |-
                      matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                      map is equivalent to an element of matchExpressions, whose key field is "key", the
                      operator is "In", and the values array contains only "value". The requirements are ANDed.
                    type: object
                type: object
                x-kubernetes-map-type: atomic
              namespaces:
                description: |-
                  Namespaces is a list of namespace names. If defined, a constraint only
                  applies to resources in a listed namespace.  Namespaces also supports a
                  prefix or suffix based glob.  For example, `+"`"+`namespaces: [kube-*]`+"`"+` matches both
                  `+"`"+`kube-system`+"`"+` and `+"`"+`kube-public`+"`"+`, and `+"`"+`namespaces: [*-system]`+"`"+` matches both
                  `+"`"+`kube-system`+"`"+` and `+"`"+`gatekeeper-system`+"`"+`.
                items:
                  description: |-
                    A string that supports globbing at its front and end. Ex: "kube-*" will match "kube-system" or
                    "kube-public", "*-system" will match "kube-system" or "gatekeeper-system", "*system*" will
                    match "system-kube" or "kube-system".  The asterisk is required for wildcard matching.
                  pattern: ^\*?[-:a-z0-9]*\*?$
                  type: string
                type: array
              scope:
                description: |-
                  Scope determines if cluster-scoped and/or namespaced-scoped resources
                  are matched.  Accepts `+"`"+`*`+"`"+`, `+"`"+`Cluster`+"`"+`, or `+"`"+`Namespaced`+"`"+`. (defaults to `+"`"+`*`+"`"+`)
                type: string
              source:
                description: |-
                  Source determines whether generated or original resources are matched.
                  Accepts `+"`"+`Generated`+"`"+`|`+"`"+`Original`+"`"+`|`+"`"+`All`+"`"+` (defaults to `+"`"+`All`+"`"+`). A value of
                  `+"`"+`Generated`+"`"+` will only match generated resources, while `+"`"+`Original`+"`"+` will only
                  match regular resources.
                enum:
                - All
                - Generated
                - Original
                type: string
            type: object
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadataDummy:
            type: object
        type: object
    served: true
    storage: true
`
