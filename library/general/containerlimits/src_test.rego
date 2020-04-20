package k8scontainerlimits

test_input_no_violations_int {
    input := {"review": review([ctr("a", 10, 20)]), "parameters": {"memory": 20, "cpu": 40}}
    results := violation with input as input
    count(results) == 0
}
test_input_no_violations_str {
    input := {"review": review([ctr("a", "10", "20")]), "parameters": {"memory": "20", "cpu": "40"}}
    results := violation with input as input
    count(results) == 0
}
test_input_no_violations_str_small {
    input := {"review": review([ctr("a", "1", "2")]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 0
}
test_input_no_violations_cpu_scale {
    input := {"review": review([ctr("a", "1", "2m")]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 0
}
test_input_violations_int {
    input := {"review": review([ctr("a", 10, 20)]), "parameters": {"memory": 5, "cpu": 10}}
    results := violation with input as input
    count(results) == 2
}

test_input_violations_mem_int_v_str {
    input := {"review": review([ctr("a", 10, "4")]), "parameters": {"memory": "1000m", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}

test_input_violations_str {
    input := {"review": review([ctr("a", "10", "20")]), "parameters": {"memory": "5", "cpu": "10"}}
    results := violation with input as input
    count(results) == 2
}
test_input_violations_str_small {
    input := {"review": review([ctr("a", "5", "6")]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 2
}
test_input_violations_cpu_scale {
    input := {"review": review([ctr("a", "1", "2")]), "parameters": {"memory": "2", "cpu": "4m"}}
    results := violation with input as input
    count(results) == 1
}
test_no_parse_cpu {
    input := {"review": review([ctr("a", "1", "212asdf")]), "parameters": {"memory": "2", "cpu": "4m"}}
    results := violation with input as input
    count(results) == 1
}
test_no_parse_ram {
    input := {"review": review([ctr("a", "1asdf", "2")]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_1_bad_cpu {
    input := {"review": review([ctr("a", "1", "2"), ctr("b", "1", "8")]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_2_bad_cpu {
    input := {"review": review([ctr("a", "1", "9"), ctr("b", "1", "8")]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 2
}
test_1_bad_ram {
    input := {"review": review([ctr("a", "1", "2"), ctr("b", "8", "2")]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_2_bad_ram {
    input := {"review": review([ctr("a", "9", "2"), ctr("b", "8", "2")]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 2
}
test_no_ram_limit {
    input := {"review": review([{"name": "a", "resources": {"limits": {"cpu": 1}}}]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_no_cpu_limit {
    input := {"review": review([{"name": "a", "resources": {"limits": {"memory": 1}}}]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_no_limit {
    input := {"review": review([{"name": "a", "resources": {}}]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_no_resources {
    input := {"review": review([{"name": "a"}]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_init_containers_checked {
    input := {"review": init_review([ctr("a", "5", "5"), ctr("b", "5", "5")]), "parameters": {"memory": "2", "cpu": "4"}}
    results := violation with input as input
    count(results) == 4
}

# MEM SCALE TESTS
test_input_no_violations_mem_K {
    input := {"review": review([ctr("a", "1", "2")]), "parameters": {"memory": "1K", "cpu": "4"}}
    results := violation with input as input
    count(results) == 0
}
test_input_violations_mem_m {
    input := {"review": review([ctr("a", "1", "2")]), "parameters": {"memory": "1m", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_K {
    input := {"review": review([ctr("a", "1K", "2")]), "parameters": {"memory": "1", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_M {
    input := {"review": review([ctr("a", "1M", "2")]), "parameters": {"memory": "1K", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_G {
    input := {"review": review([ctr("a", "1G", "2")]), "parameters": {"memory": "1M", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_T {
    input := {"review": review([ctr("a", "1T", "2")]), "parameters": {"memory": "1G", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_P {
    input := {"review": review([ctr("a", "1P", "2")]), "parameters": {"memory": "1T", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_E {
    input := {"review": review([ctr("a", "1E", "2")]), "parameters": {"memory": "1P", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Ki {
    input := {"review": review([ctr("a", "1Ki", "2")]), "parameters": {"memory": "1", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Mi {
    input := {"review": review([ctr("a", "1Mi", "2")]), "parameters": {"memory": "1Ki", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Gi {
    input := {"review": review([ctr("a", "1Gi", "2")]), "parameters": {"memory": "1Mi", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Ti {
    input := {"review": review([ctr("a", "1Ti", "2")]), "parameters": {"memory": "1Gi", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Pi {
    input := {"review": review([ctr("a", "1Pi", "2")]), "parameters": {"memory": "1Ti", "cpu": "4"}}
    results := violation with input as input
    count(results) == 1
}
test_input_violations_mem_Ei {
    input := {"review": review([ctr("a", "1Ei", "2")]), "parameters": {"memory": "1Pi", "cpu": "4"}}
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

ctr(name, mem, cpu) = out {
  out = {"name": name, "resources": {"limits": {"memory": mem, "cpu": cpu}}}
}
