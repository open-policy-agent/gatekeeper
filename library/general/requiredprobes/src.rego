package k8srequiredprobes

violation[{"msg": msg}] {
    input.review.kind.kind == "Deployment"
    container := input.review.object.spec.template.spec.containers[_]
    probe := input.parameters.probes[_]
    not container[probe]
    msg := get_violation_message(container, input.review, probe)
}

violation[{"msg": msg}] {
    input.review.kind.kind == "DaemonSet"
    container := input.review.object.spec.template.spec.containers[_]
    probe := input.parameters.probes[_]
    not container[probe]
    msg := get_violation_message(container, input.review, probe)
}

violation[{"msg": msg}] {
    input.review.kind.kind == "StatefulSet"
    container := input.review.object.spec.template.spec.containers[_]
    probe := input.parameters.probes[_]
    not container[probe]
    msg := get_violation_message(container, input.review, probe)
}

violation[{"msg": msg}] {
    input.review.kind.kind == "ReplicaSet"
    container := input.review.object.spec.template.spec.containers[_]
    probe := input.parameters.probes[_]
    not container[probe]
    msg := get_violation_message(container, input.review, probe)
}

violation[{"msg": msg}] {
    input.review.kind.kind == "Pod"
    container := input.review.object.spec.containers[_]
    probe := input.parameters.probes[_]
    not container[probe]
    msg := get_violation_message(container, input.review, probe)
}

get_violation_message(container, review, probe) = msg {
    msg := sprintf("CONTAINER <%v> in your <%v> <%v> HAS NO <%v>", [container.name, review.kind.kind, review.object.metadata.name, probe])
}
