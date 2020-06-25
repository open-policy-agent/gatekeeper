package k8spspselinux

# Disallow top level custom SELinux options
violation[{"msg": msg, "details": {}}] {
    has_field(input.review.object.spec.securityContext, "seLinuxOptions")
    not input_seLinuxOptions_allowed(input.review.object.spec.securityContext.seLinuxOptions)
    msg := sprintf("SELinux options is not allowed, pod: %v. Allowed options: %v", [input.review.object.metadata.name, input.parameters.allowedSELinuxOptions])
}
# Disallow container level custom SELinux options
violation[{"msg": msg, "details": {}}] {
    c := input_security_context[_]
    has_field(c.securityContext, "seLinuxOptions")
    not input_seLinuxOptions_allowed(c.securityContext.seLinuxOptions)
    msg := sprintf("SELinux options is not allowed, pod: %v, container %v. Allowed options: %v", [input.review.object.metadata.name, c.name, input.parameters.allowedSELinuxOptions])
}
input_seLinuxOptions_allowed(options) {
    field_allowed(options, "level")
    field_allowed(options, "role")
    field_allowed(options, "type")
    field_allowed(options, "user")
}

field_allowed(options, field) {
    matching := {1 | input.parameters.allowedSELinuxOptions[_][field] == options[field]}
    count(matching) > 0
}
field_allowed(options, field) {
    not has_field(options, field)
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
