package k8suniqueingresshost
       
violation[{"msg": msg}] {
    input.review.kind.kind == "Ingress"              
    host := input.review.object.spec.rules[_].host
    other := data.inventory.namespace[ns][_]["Ingress"][name]
    other.spec.rules[_].host == host
    not identical(other, input.review)
    msg := sprintf("ingress host conflicts with an existing ingress <%v>", [host])
}

identical(obj, review) {
    obj.metadata.namespace == review.object.metadata.namespace
    obj.metadata.name == review.object.metadata.name
}