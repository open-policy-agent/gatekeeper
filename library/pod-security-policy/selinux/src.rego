package k8spspselinux

violation[{"msg": msg, "details": {}}] {
    # Disallow top level custom SELinux options
    input.review.object.spec.securityContext.seLinuxOptions
    msg := sprintf("Setting SELinux options is not allowed, pod: %v", [input.review.object.metadata.name])
}
violation[{"msg": msg, "details": {}}] {
    c := input_security_context[_]
    # Disallow container level custom SELinux options
    c.securityContext.seLinuxOptions
    msg := sprintf("Setting SELinux options is not allowed, pod: %v, container %v", [input.review.object.metadata.name, c.name])
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
