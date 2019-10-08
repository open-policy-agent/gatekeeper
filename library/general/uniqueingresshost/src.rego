package k8suniqueingresshost

identical(obj, review) {
  obj.metadata.namespace == review.object.metadata.namespace
  obj.metadata.name == review.object.metadata.name
}

make_apiversion(kind) = apiVersion {
  g := kind.group
  v := kind.version
  g != ""
  apiVersion := sprintf("%v/%v", [g, v])
}

make_apiversion(kind) = apiVersion {
  kind.group == ""
  apiVersion := kind.version
}

violation[{"msg": msg}] {
  input.review.kind.kind == "Ingress"
  apiVersion := make_apiversion(input.review.kind)
  apis := ["extensions/v1beta1", "networking.k8s.io/v1beta1"]
  apiVersion == apis[_]
  host := input.review.object.spec.rules[_].host
  other := data.inventory.namespace[ns][otherapi]["Ingress"][name]
  otherapi == apis[_]
  other.spec.rules[_].host == host
  not identical(other, input.review)
  msg := sprintf("ingress host conflicts with an existing ingress <%v>", [host])
}
