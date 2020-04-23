package k8spspapparmor

violation[{"msg": msg, "details": {}}] {
    metadata := input.review.object.metadata
    container := input_containers[_]
    not input_apparmor_allowed(container, metadata)
    msg := sprintf("AppArmor profile is not allowed, pod: %v, container: %v. Allowed profiles: %v", [input.review.object.metadata.name, container.name, input.parameters.allowedProfiles])
}

input_apparmor_allowed(container, metadata) {
    metadata.annotations[key] == input.parameters.allowedProfiles[_]
    key == sprintf("container.apparmor.security.beta.kubernetes.io/%v", [container.name])
}

input_containers[c] {
    c := input.review.object.spec.containers[_]
}
input_containers[c] {
    c := input.review.object.spec.initContainers[_]
}
