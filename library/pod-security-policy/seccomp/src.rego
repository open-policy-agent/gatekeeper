package k8spspseccomp

violation[{"msg": msg, "details": {}}] {
    metadata := input.review.object.metadata
    not input_seccomp_allowed(metadata)
    msg := sprintf("Seccomp profile is not allowed, pod: %v. Allowed profiles: %v", [input.review.object.metadata.name, input.parameters.allowedProfiles])
}

input_seccomp_allowed(metadata) {
    metadata.annotations
    not metadata.annotations["seccomp.security.alpha.kubernetes.io/pod"]
}

input_seccomp_allowed(metadata) {
    input.parameters.allowedProfiles[_] == "*"
}

input_seccomp_allowed(metadata) {
    metadata.annotations["seccomp.security.alpha.kubernetes.io/pod"] == input.parameters.allowedProfiles[i]
}
