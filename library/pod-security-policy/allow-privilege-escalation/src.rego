package k8spspallowprivilegeescalationcontainer

violation[{"msg": msg, "details": {}}] {
    c := input_containers[_]
    input_allow_privilege_escalation(c, input.review.object)
    msg := sprintf("Privilege escalation container is not allowed: %v", [c.name])
}

input_allow_privilege_escalation(c, review_object) {
    not has_field(c, "securityContext")
    not has_field(review_object.spec, "securityContext")
}
input_allow_privilege_escalation(c, review_object) {
    not has_field(c, "securityContext")
    has_field(review_object.spec, "securityContext")
    not review_object.spec.securityContext.allowPrivilegeEscalation == false
}
input_allow_privilege_escalation(c, review_object) {
    has_field(c, "securityContext")
    not c.securityContext.allowPrivilegeEscalation == false
}
input_containers[c] {
    c := input.review.object.spec.containers[_]
}
input_containers[c] {
    c := input.review.object.spec.initContainers[_]
}
# has_field returns whether an object has a field
has_field(object, field) = true {
    object[field]
}
