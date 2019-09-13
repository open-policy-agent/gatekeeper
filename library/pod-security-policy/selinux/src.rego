package k8spspselinux

violation[{"msg": msg, "details": {}}] {
    c := input_security_context[_]
    not input_seLinuxOptions_allowed(c.securityContext.seLinuxOptions)
    msg := sprintf("SELinux option is not allowed, pod: %v. Allowed options: %v", [input.review.object.metadata.name, input.parameters.allowedSELinuxOptions])
}

input_seLinuxOptions_allowed(options) {
    input.parameters.allowedSELinuxOptions.level == options.level
}

input_seLinuxOptions_allowed(options) {
    input.parameters.allowedSELinuxOptions.role == options.role
}

input_seLinuxOptions_allowed(options) {
    input.parameters.allowedSELinuxOptions.type == options.type
}

input_seLinuxOptions_allowed(options) {
    input.parameters.allowedSELinuxOptions.user == options.user
}

input_security_context[c] {
    c := input.review.object.spec
}
input_security_context[c] {
    c := input.review.object.spec.containers[_]
    has_field(c.securityContext, "seLinuxOptions")
}
input_security_context[c] {
    c := input.review.object.spec.initContainers[_]
    has_field(c.securityContext, "seLinuxOptions")
}

# has_field returns whether an object has a field
has_field(object, field) = true {
    object[field]
}
