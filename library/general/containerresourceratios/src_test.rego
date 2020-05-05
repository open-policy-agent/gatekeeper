package k8scontainerratios

test_input_no_violations_int {
    input := {"review": review([ctr("a", 10, 20, 5, 10)]), "parameters": {"ratio": 2}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 0
}
test_input_no_violations_str {
    input := {"review": review([ctr("a", "10", "20", "5", "10")]), "parameters": {"ratio": "2"}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 0
}
test_input_no_violations_str_small {
    input := {"review": review([ctr("a", "1", "2", "1", "1")]), "parameters": {"ratio": "2"}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 0
}
test_input_no_violations_cpu_scale {
    input := {"review": review([ctr("a", "2", "4m", "1", "2m")]), "parameters": {"ratio": "2"}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 0
}
test_input_no_violations_cpu_decimal {
    input := {"review": review([ctr("a", "2", "3", "1", "1.5")]), "parameters": {"ratio": "2"}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 0
}
test_input_violations_int {
    input := {"review": review([ctr("a", 20, 40, 5, 10)]), "parameters": {"ratio": 2}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 2
}
test_input_violations_mem_int_v_str {
    input := {"review": review([ctr("a", 1, "3", "1m", "1.5")]), "parameters": {"ratio": "2"}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 1
}
test_input_violations_str {
    input := {"review": review([ctr("a", "10", "20", "2", "4")]), "parameters": {"ratio": "2"}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 2
}
test_input_violations_str_small {
    input := {"review": review([ctr("a", "5", "6", "1", "1")]), "parameters": {"ratio": "3"}}
    results := violation with input as input
    count(results) == 2
}
test_input_violations_cpu_scale {
    input := {"review": review([ctr("a", "1", "2", "1", "4m")]), "parameters": {"ratio": "10"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_cpu_decimal {
    input := {"review": review([ctr("a", "1", "2", "1", "0.5")]), "parameters": {"ratio": "2"}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 1
}
test_no_parse_cpu_limits {
    input := {"review": review([ctr("a", "1", "212asdf", "2", "2")]), "parameters": {"raio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_no_parse_cpu_requests {
    input := {"review": review([ctr("a", "1", "2", "2", "212asdf")]), "parameters": {"raio": "4"}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 1
}
test_no_parse_cpu_requests_and_limits {
    input := {"review": review([ctr("a", "1", "212asdf", "2", "212asdf")]), "parameters": {"raio": "4"}}
    results := violation with input as input
    count(results) == 2
}
test_no_parse_ram_limits {
    input := {"review": review([ctr("a", "1asdf", "2", "1", "2")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_no_parse_ram_requests {
    input := {"review": review([ctr("a", "1", "2", "1asdf", "2")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_no_parse_ram_requests_and_limits {
    input := {"review": review([ctr("a", "1asdf", "2", "1asdf", "2")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 2
}
test_1_bad_cpu {
    input := {"review": review([ctr("a", "1", "2", "1", "2"), ctr("b", "1", "8", "1", "2")]), "parameters": {"ratio": "2"}}
    results := violation with input as input
    count(results) == 1
}
test_2_bad_cpu {
    input := {"review": review([ctr("a", "1", "9", "1", "3"), ctr("b", "1", "8", "1", "2")]), "parameters": {"ratio": "2"}}
    results := violation with input as input
    count(results) == 2
}
test_1_bad_ram {
    input := {"review": review([ctr("a", "1", "2", "1" ,"2"), ctr("b", "8", "2", "2", "2")]), "parameters": {"ratio": "1"}}
    results := violation with input as input
    count(results) == 1
}
test_2_bad_ram {
    input := {"review": review([ctr("a", "9", "2", "3", "2"), ctr("b", "8", "2", "2", "2")]), "parameters": {"ratio": "2"}}
    results := violation with input as input
    count(results) == 2
}
test_no_ram_limit {
    input := {"review": review([{"name": "a", "resources": {"limits": {"cpu": 1}}}]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 2
}
test_no_cpu_limit {
    input := {"review": review([{"name": "a", "resources": {"limits": {"memory": 1}}}]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 2
}
test_no_limit {
    input := {"review": review([{"name": "a", "resources": {}}]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 2
}
test_no_ram_request {
    input := {"review": review([{"name": "a", "resources": {"requests": {"cpu": 1}}}]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 2
}
test_no_cpu_request {
    input := {"review": review([{"name": "a", "resources": {"requests": {"memory": 1}}}]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 2
}
test_no_resources {
    input := {"review": review([{"name": "a"}]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    trace(sprintf("results - <%v>", [results]))
    count(results) == 2
}
test_init_containers_checked {
    input := {"review": init_review([ctr("a", "5", "5", "1", "1"), ctr("b", "5", "5", "1", "1")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 4
}
# MEM SCALE TESTS
test_input_no_violations_mem_K {
    input := {"review": review([ctr("a", "1k", "2", "1k", "2")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 0
}
test_input_violations_mem_K {
    input := {"review": review([ctr("a", "4k", "2", "1k", "2")]), "parameters": {"ratio": "2"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_m {
    input := {"review": review([ctr("a", "1", "2", "1m", "2")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_M {
    input := {"review": review([ctr("a", "1M", "2", "1k", "2")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_G {
    input := {"review": review([ctr("a", "1G", "2", "1M", "1")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_T {
    input := {"review": review([ctr("a", "1T", "2", "1G", "1")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_P {
    input := {"review": review([ctr("a", "1P", "2", "1T", "2")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_E {
    input := {"review": review([ctr("a", "1E", "2", "1P", "2")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Ki {
    input := {"review": review([ctr("a", "1Ki", "2", "1", "2")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Mi {
    input := {"review": review([ctr("a", "1Mi", "2", "1Ki", "1")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Gi {
    input := {"review": review([ctr("a", "1Gi", "2", "1Mi", "2")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Ti {
    input := {"review": review([ctr("a", "1Ti", "2", "1Gi", "1")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Pi {
    input := {"review": review([ctr("a", "1Pi", "2", "1Ti", "1")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Ei {
    input := {"review": review([ctr("a", "1Ei", "2", "1Pi", "1")]), "parameters": {"ratio": "4"}}
    results := violation with input as input
    count(results) == 1
}

review(containers) = output {
  output = {
    "object": {
      "metadata": {
        "name": "nginx",
      },
      "spec": {"containers": containers}
    }
  }
}

init_review(containers) = output {
  output = {
    "object": {
      "metadata": {
        "name": "nginx",
      },
      "spec": {"initContainers": containers}
    }
  }
}

ctr(name, mem_limits, cpu_limits, mem_requests, cpu_requests) = out {
  out = {"name": name, "resources": {"limits": {"memory": mem_limits, "cpu": cpu_limits},"requests": {"memory": mem_requests, "cpu": cpu_requests}}}
}