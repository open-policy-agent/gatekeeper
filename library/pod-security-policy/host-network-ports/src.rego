package k8spsphostnetworkingports

violation[{"msg": msg, "details": {}}] {
    input_share_hostnetwork(input.review.object)
    msg := sprintf("The specified hostNetwork and hostPort are not allowed, pod: %v. Allowed values: %v", [input.review.object.metadata.name, input.parameters])
}

input_share_hostnetwork(o) {
    not input.parameters.hostNetwork
    o.spec.hostNetwork
}

input_share_hostnetwork(o) {
    hostPort := input_containers[_].ports[_].hostPort
    hostPort < input.parameters.min
}

input_share_hostnetwork(o) {
    hostPort := input_containers[_].ports[_].hostPort
    hostPort > input.parameters.max
}

input_containers[c] {
    c := input.review.object.spec.containers[_]
}

input_containers[c] {
    c := input.review.object.spec.initContainers[_]
}
