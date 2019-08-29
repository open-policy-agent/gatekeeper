package k8spspapparmor

violation[{"msg": msg, "details": {}}] {
    metadata := input.review.object.metadata
    not input_apparmor_allowed(metadata)
    msg := sprintf("apparmor profile is not allowed, pod: %v. Allowed profiles: %v", [input.review.object.metadata.name, input.parameters.allowedProfiles])
}

input_apparmor_allowed(metadata) {
    metadata.annotations
    not metadata.annotations["apparmor.security.beta.kubernetes.io/pod"]
}

input_apparmor_allowed(metadata) {
    metadata.annotations["apparmor.security.beta.kubernetes.io/pod"] == input.parameters.allowedProfiles[i]
}
