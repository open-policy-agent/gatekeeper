package k8srequiredprobes

test_one_ctr_no_violations {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container","image": "my-image:latest","readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 0
}

test_one_ctr_readiness_violation {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_one_ctr_liveness_violation {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_one_ctr_all_violations {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container","image": "my-image:latest"}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_no_violations {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest","readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest","readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 0
}

test_two_ctrs_all_violations_in_both {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest"},
                                {"name": "my-container2","image": "my-image:latest"}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 4
}

test_two_ctrs_all_violations_in_ctr_one {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest"},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_all_violations_in_ctr_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest"}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_readiness_violation_in_ctr_one {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest","readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_readiness_violation_in_ctr_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}},
                                {"name": "my-container2","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_readiness_violation_in_both {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_liveness_violation_in_ctr_one {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_liveness_violation_in_ctr_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_liveness_violation_in_both {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_readiness_in_one_liveness_in_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest","readinessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_liveness_in_one_readiness_in_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_readiness_violation_in_ctr_one_all_violations_in_ctr_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest"}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}

test_two_ctrs_liveness_violation_in_ctr_one_all_violations_in_ctr_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest"}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}

test_two_ctrs_readiness_violation_in_ctr_two_all_violations_in_ctr_one {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest"},
                                {"name": "my-container2","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}

test_two_ctrs_liveness_violation_in_ctr_two_all_violations_in_ctr_one {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest"},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}

test_one_ctr_empty_readiness_violation {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_one_ctr_empty_liveness_violation {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_one_ctr_empty_probes_violations {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_empty_probes_violation_in_both {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 4
}

test_two_ctrs_empty_probes_violation_in_ctr_one {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_empty_probes_violation_in_ctr_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_empty_readiness_violation_in_ctr_one {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "livenessProbe": {"tcpSocket": {"port":80}}, "readinessProbe": {}},
                                {"name": "my-container2","image": "my-image:latest","readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_empty_readiness_violation_in_ctr_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_empty_readiness_violation_in_both {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_empty_liveness_violation_in_ctr_one {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_empty_liveness_violation_in_ctr_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {"tcpSocket": {"port":8080}}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 1
}

test_two_ctrs_empty_liveness_violation_in_both {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_empty_readiness_in_ctr_one_empty_liveness_in_ctr_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe":{}, "livenessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":8080}}, "livenessProbe": {}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_empty_liveness_in_one_empty_readiness_in_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {"tcpSocket": {"port":8080}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 2
}

test_two_ctrs_empty_readiness_in_ctr_one_both_empty_probes_in_ctr_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {"tcpSocket": {"port":80}}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}

test_two_ctrs_empty_liveness_in_ctr_one_both_empty_probes_in_ctr_two {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}

test_two_ctrs_empty_readiness_in_ctr_two_both_empty_probes_in_ctr_one {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {"tcpSocket": {"port":80}}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}

test_two_ctrs_empty_liveness_in_ctr_two_both_empty_probes_in_ctr_one {
    kind := kinds[_]
    input := {"review": review([{"name": "my-container1","image": "my-image:latest", "readinessProbe": {}, "livenessProbe": {}},
                                {"name": "my-container2","image": "my-image:latest", "readinessProbe": {"tcpSocket": {"port":80}}, "livenessProbe": {}}]),
              "parameters": parameters}
    results := violation with input as input
    count(results) == 3
}

review(containers) = obj {
    obj = {
            "kind": {
                "kind": "Pod"
            },
            "object": {
                "metadata": {
                    "name": "some-name"
                },
                "spec": {
                    "containers":containers
                }
            }
        }
}

parameters = {"probes": ["readinessProbe", "livenessProbe"], "probeTypes": ["tcpSocket", "httpGet", "exec"]}
kinds = ["Pod"]
