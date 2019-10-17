package k8spspapparmor

violation[{"msg": msg, "details": {}}] {
    metadata := input.review.object.metadata
    not input_apparmor_allowed(metadata)
    msg := sprintf("AppArmor profile is not allowed, pod: %v. Allowed profiles: %v", [input.review.object.metadata.name, input.parameters.allowedProfiles])
}

input_apparmor_allowed(metadata) {
    metadata.annotations[key] == input.parameters.allowedProfiles[_]
    startswith(key, "container.apparmor.security.beta.kubernetes.io/")
    [prefix, name] := split(key, "/")
    name == input_containers[_].name
}

input_containers[c] {
    c := input.review.object.spec.containers[_]
}
input_containers[c] {
    c := input.review.object.spec.initContainers[_]
}
