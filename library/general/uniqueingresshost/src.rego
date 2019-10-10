package k8suniqueingresshost

identical(obj, review) {
  obj.metadata.namespace == review.object.metadata.namespace
  obj.metadata.name == review.object.metadata.name
}

violation[{"msg": msg}] {
  input.review.kind.kind == "Ingress"
  re_match("^(extensions|networking.k8s.io)$", input.review.kind.group)
  host := input.review.object.spec.rules[_].host
  other := data.inventory.namespace[ns][otherapiversion]["Ingress"][name]
  re_match("^(extensions|networking.k8s.io)/.+$", otherapiversion)
  other.spec.rules[_].host == host
  not identical(other, input.review)
  msg := sprintf("ingress host conflicts with an existing ingress <%v>", [host])
}
