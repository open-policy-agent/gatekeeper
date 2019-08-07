package k8spspallowprivilegeescalationcontainer

violation[{"msg": msg, "details": {}}] {
    c := input_containers[_]
    input_allow_privilege_escalation(c)
    msg := sprintf("Privilege escalation container is not allowed: %v", [c.name])
}

input_allow_privilege_escalation(c) {
    not has_field(c, "securityContext")
}
input_allow_privilege_escalation(c) {
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
