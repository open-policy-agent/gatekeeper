package k8sazureallowedseccomp

violation[{"msg": msg, "details": {}}] {
    metadata := input.review.object.metadata
    not input_wildcard_allowed(metadata)
    container := input_containers[_]
    not input_container_allowed(metadata, container)
    msg := sprintf("Seccomp profile is not allowed, pod: %v, container: %v, Allowed profiles: %v", [metadata.name, container.name, input.parameters.allowedProfiles])
}

input_wildcard_allowed(metadata) {
    input.parameters.allowedProfiles[_] == "*"
}

input_container_allowed(metadata, container) {
    not get_container_profile(metadata, container)
    metadata.annotations["seccomp.security.alpha.kubernetes.io/pod"] == input.parameters.allowedProfiles[_]
}

input_container_allowed(metadata, container) {
	profile := get_container_profile(metadata, container)
	profile == input.parameters.allowedProfiles[_]
}

get_container_profile(metadata, container) = profile {
	value := metadata.annotations[key]
    startswith(key, "container.seccomp.security.alpha.kubernetes.io/")
    [prefix, name] := split(key, "/")
    name == container.name
    profile = value
}

input_containers[c] {
    c := input.review.object.spec.containers[_]
}
input_containers[c] {
    c := input.review.object.spec.initContainers[_]
}
