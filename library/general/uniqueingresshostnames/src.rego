package k8suniqueingresshostnames

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

violation[{"msg": msg}] {
  input.review.kind.kind == "Ingress"
  input.review.kind.version == "v1"
  input.review.kind.group == ""
  input_host := input.review.object.spec.rules[_].host
  other := data.inventory.namespace[namespace][_][_][name]
  not identical(other, input.review)
  other_host := other.spec.rules[_].host
  input_host == other_host
  msg := sprintf("Ingress host conflicts with an existing ingress <%v> in namespace <%v>", [name, namespace])
}
