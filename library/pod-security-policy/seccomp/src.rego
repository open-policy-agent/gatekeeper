package k8spspseccomp

violation[{"msg": msg, "details": {}}] {
    metadata := input.review.object.metadata
    not input_seccomp_allowed(metadata)
    msg := sprintf("Seccomp profile is not allowed, pod: %v. Allowed profiles: %v", [input.review.object.metadata.name, input.parameters.allowedProfiles])
}

input_seccomp_allowed(metadata) {
    input.parameters.allowedProfiles[_] == "*"
}

input_seccomp_allowed(metadata) {
    metadata.annotations["seccomp.security.alpha.kubernetes.io/pod"] == input.parameters.allowedProfiles[_]
}

input_seccomp_allowed(metadata) {
    metadata.annotations[key] == input.parameters.allowedProfiles[_]
    startswith(key, "container.seccomp.security.alpha.kubernetes.io/")
    [prefix, name] := split(key, "/")
    name == input_containers[_].name
}

input_containers[c] {
    c := input.review.object.spec.containers[_]
}
input_containers[c] {
    c := input.review.object.spec.initContainers[_]
}
