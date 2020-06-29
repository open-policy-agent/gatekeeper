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
    params := input.parameters.allowedSELinuxOptions[_]
    field_allowed("level", options, params)
    field_allowed("role", options, params)
    field_allowed("type", options, params)
    field_allowed("user", options, params)
}

field_allowed(field, options, params) {
    params[field] == options[field]
}
field_allowed(field, options, params) {
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
