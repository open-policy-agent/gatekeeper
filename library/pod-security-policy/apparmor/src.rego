package k8spspapparmor

violation[{"msg": msg, "details": {}}] {
    metadata := input.review.object.metadata
    not input_apparmor_allowed(metadata)
    msg := sprintf("AppArmor profile is not allowed, pod: %v. Allowed profiles: %v", [input.review.object.metadata.name, input.parameters.allowedProfiles])
}

input_apparmor_allowed(metadata) {
    metadata.annotations[key]
    [prefix, name] = split_annotation(key)
    prefix == "container.apparmor.security.beta.kubernetes.io"
    annotation := sprintf("container.apparmor.security.beta.kubernetes.io/%v", [name])
    name == input_containers[_].name
    metadata.annotations[annotation] == input.parameters.allowedProfiles[_]
}

input_containers[c] {
    c := input.review.object.spec.containers[_]
}
input_containers[c] {
    c := input.review.object.spec.initContainers[_]
}

split_annotation(annotation) = [prefix, name] {
    [prefix, name] = split(annotation, "/")
}
