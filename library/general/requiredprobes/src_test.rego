package k8srequiredprobes

test_one_ctr_no_violations {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container","image": "my-image:latest","readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 0
}

test_one_ctr_readiness_violation {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_one_ctr_liveness_violation {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_one_ctr_all_violations {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container","image": "my-image:latest"}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_no_violations {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest","readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {"tcpSocket": {"port":80}}},
                                      {"name": "my-container2","image": "my-image:latest","readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 0
}

test_two_ctrs_all_violations_in_both {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest"},
                                      {"name": "my-container2","image": "my-image:latest"}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 4
}

test_two_ctrs_all_violations_in_ctr_one {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest"},
                                      {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_all_violations_in_ctr_two {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {"tcpSocket": {"port":80}}},
                                      {"name": "my-container2","image": "my-image:latest"}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_readiness_violation_in_ctr_one {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}},
                                      {"name": "my-container2","image": "my-image:latest","readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_readiness_violation_in_ctr_two {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}},
                                      {"name": "my-container2","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_readiness_violation_in_both {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}},
                                      {"name": "my-container2","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_liveness_violation_in_ctr_one {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}},
                                      {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_liveness_violation_in_ctr_two {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}},
                                      {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_liveness_violation_in_both {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}},
                                      {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_readiness_in_one_liveness_in_two {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}},
                                      {"name": "my-container2","image": "my-image:latest","readinessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_liveness_in_one_readiness_in_two {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}},
                                      {"name": "my-container2","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_readiness_violation_in_ctr_one_all_violations_in_ctr_two {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}},
                                      {"name": "my-container2","image": "my-image:latest"}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}

test_two_ctrs_liveness_violation_in_ctr_one_all_violations_in_ctr_two {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}},
                                      {"name": "my-container2","image": "my-image:latest"}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}

test_two_ctrs_readiness_violation_in_ctr_two_all_violations_in_ctr_one {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest"},
                                      {"name": "my-container2","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}

test_two_ctrs_liveness_violation_in_ctr_two_all_violations_in_ctr_one {
    kind := kinds[_]
    input := {"review": review(kind, [{"name": "my-container1","image": "my-image:latest"},
                                      {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}


review(kind, containers) = {"kind":{"kind":kind},"object":{"metadata":{"name":"some-name"},"spec":{"template":{"spec":{"containers":containers}}}}} {
    not kind == "Pod"
} else = {"kind":{"kind":kind},"object":{"metadata":{"name":"some-name"},"spec":{"containers":containers}}} {
    kind == "Pod"
}

parameters = {"probes": ["readinessProbe", "livenessProbe"]}
kinds = ["Deployment", "ReplicaSet", "DaemonSet", "StatefulSet", "Pod"]
