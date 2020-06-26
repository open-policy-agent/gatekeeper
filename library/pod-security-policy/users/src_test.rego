package k8spspallowedusers


## Test Functionality ##
# All the following functions will omit a field if the argument is null

## Input Functions ##
# "review":
#   review(<pod-level-security-context>, <pod-containers>, <pod-init-containers>)
#     - Returns an pod review object with "spec" = { "securityContext": <pod-level-security-context>, "containers": <pod-containers>, "initContainers": <pod-init-containers> }
#     - omits any field where the value is null
#   ctr(<container-name>, <container-security-context>)
#     - Returns a container object containing { "name": <container-name>, "securityContext": <container-security-context> }
#     - omits "securityContext" object if argument is null

# "parameters":
#   rule(<rule-name>, <rule-ranges>)
#     - Returns a rule object with { "rule": <rule-name>, "ranges": <rule-ranges> }
#     - omits "ranges" field if <rule-ranges> is null
#   range(<min-val>, <max-val>)
#     - Returns a range object with  { "min": <min-val>, "max": <max-val> }
# Wrap rule in runAsUser, or 

## Other Functions ##
# run_as_rule("runAsUser" value, "runAsGroup" value, "supplementalGroups" value, "fsGroup" value)
#   - Returns {"runAsUser": value, "runAsGroup": value, "supplementalGroups": value, "fsGroup": value }
#   - omits any field where value is null
# runAsUser(value)
#   - Returns { "runAsUser": value} if value is not null, else returns {}
# runAsGroup(value)
#   - Returns { "runAsGroup": value} if value is not null, else returns {}
# supplementalGroups(value)
#   - Returns { "supplementalGroups": value} if value is not null, else returns {}
# fsGroup(value)
#   - Returns { "fsGroup": value} if value is not null, else returns {}

## Rego Playground for generating object: https://play.openpolicyagent.org/p/s64GOBr0Dq


## RunAsUser ##
test_user_one_container_run_as_rule_any {
  input := { "review": review(null, [ctr("cont1", runAsUser(12))], null), "parameters": user_runasany }
  results := violation with input as input
  count(results) == 0
}
test_user_one_container_run_as_rule_any_root_user {
  input := { "review": review(null, [ctr("cont1", runAsUser(0))], null), "parameters": user_runasany }
  results := violation with input as input
  count(results) == 0
}
test_user_one_container_run_as_rule_non_root_user_is_not_root {
  input := { "review": review(null, [ctr("cont1", runAsUser(1))], null), "parameters": user_mustrunasnonroot }
  results := violation with input as input
  count(results) == 0
}
test_user_one_container_run_as_rule_non_root_user_is_root {
  input := { "review": review(null, [ctr("cont1", runAsUser(0))], null), "parameters": user_mustrunasnonroot }
  results := violation with input as input
  count(results) == 1
}
test_user_one_container_run_in_range_user_in_range {
  input := { "review": review(null, [ctr("cont1", runAsUser(150))], null), "parameters": user_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_user_one_container_run_in_range_user_out_of_range {
  input := { "review": review(null, [ctr("cont1", runAsUser(10))], null), "parameters": user_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_user_one_container_run_in_range_user_lower_edge_of_range {
  input := { "review": review(null, [ctr("cont1", runAsUser(100))], null), "parameters": user_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_user_one_container_run_in_range_user_upper_edge_of_range {
  input := { "review": review(null, [ctr("cont1", runAsUser(200))], null), "parameters": user_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_user_one_container_run_in_range_user_between_ranges {
  input := { "review": review(null, [ctr("cont1", runAsUser(200))], null), "parameters": user_mustrunas_two_ranges }
  results := violation with input as input
  count(results) == 1
}
test_user_two_containers_run_as_rule_any {
  input := {
    "review": review(null, [ctr("cont1", runAsUser(1)), ctr("cont2", runAsUser(100))], null),
    "parameters": user_runasany
  }
  results := violation with input as input
  count(results) == 0
}
test_user_two_containers_run_as_rule_non_root_users_are_not_root {
  input := {
    "review": review(null, [ctr("cont1", runAsUser(1)), ctr("cont2", runAsUser(100))], null),
    "parameters": user_mustrunasnonroot
  }
  results := violation with input as input
  count(results) == 0
}
test_user_two_containers_run_as_rule_non_root_one_is_root {
  input := {
    "review": review(null, [ctr("cont1", runAsUser(1)), ctr("cont2", runAsUser(0))], null),
    "parameters": user_mustrunasnonroot
  }
  results := violation with input as input
  count(results) == 1
}
test_user_two_containers_run_in_range_both_in_range {
  input := {
    "review": review(null, [ctr("cont1", runAsUser(150)), ctr("cont2", runAsUser(103))], null),
    "parameters": user_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 0
}
test_user_two_containers_run_in_range_one_in_range {
  input := {
    "review": review(null, [ctr("cont1", runAsUser(150)), ctr("cont2", runAsUser(13))], null),
    "parameters": user_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 1
}
test_user_two_containers_run_in_range_neither_in_range_two_ranges {
  input := {
    "review": review(null, [ctr("cont1", runAsUser(150)), ctr("cont2", runAsUser(130))], null),
    "parameters": user_mustrunas_two_ranges
  }
  results := violation with input as input
  count(results) == 2
}
test_user_one_container_one_initcontainer_run_in_range_user_in_range {
  input := {
    "review": review(null, [ctr("cont1", runAsUser(150))], [ctr("init1", runAsUser(150))]),
    "parameters": user_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 0
}
test_user_one_container_one_initcontainer_run_in_range_user_not_in_range {
  input := {
    "review": review(null, [ctr("cont1", runAsUser(150))], [ctr("init1", runAsUser(250))]),
    "parameters": user_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 1
}
test_user_one_container_empty_security_context {
  input := { "review": review(null, [ctr("cont1", {})], null), "parameters": user_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_user_one_container_no_security_context {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": user_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_user_one_container_no_security_context_RunAsAny {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": user_runasany }
  results := violation with input as input
  count(results) == 0
}
test_user_one_container_empty_security_context_empty_pod_security_context {
  input := { "review": review({}, [ctr("cont1", {})], null), "parameters": user_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_user_one_container_no_security_context_no_pod_security_context {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": user_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_user_one_container_pod_defined_null_container_run_in_range_user_in_range {
  input := { "review": review(runAsUser(150), [ctr("cont1", null)], null), "parameters": user_mustrunas_100_200}
  results := violation with input as input
  count(results) == 0
}
test_user_one_container_pod_defined_run_in_range_user_in_range {
  input := { "review": review(runAsUser(150), [ctr("cont1", {})], null), "parameters": user_mustrunas_100_200}
  results := violation with input as input
  count(results) == 0
}
test_user_one_container_pod_defined_run_in_range_user_not_in_range {
  input := { "review": review(runAsUser(250), [ctr("cont1", {})], null), "parameters": user_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_user_one_container_pod_defined_run_in_range_container_overrides_user_in_range {
  input := { "review": review(runAsUser(250), [ctr("cont1", runAsUser(150))], null), "parameters": user_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_user_one_container_pod_defined_run_in_range_container_overrides_user_not_in_range {
  input := { "review": review(runAsUser(150), [ctr("cont1", runAsUser(250))], null), "parameters": user_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_user_input_container_run_as_rule_any_ignore_ranges {
  input := {
    "review": review(null, [ctr("cont1", runAsUser(10))], null),
    "parameters": runAsUser(rule("RunAsAny", [range(100, 200)]))
  }
  results := violation with input as input
  count(results) == 0
}
test_user_input_container_run_as_rule_non_root_ignore_ranges {
  input := {
    "review": review(null, [ctr("cont1", runAsUser(10))], null),
    "parameters": runAsUser(rule("MustRunAsNonRoot", [range(100, 200)]))
  }
  results := violation with input as input
  count(results) == 0
}



## runAsGroup ##
test_group_one_container_run_as_rule_any {
  input := { "review": review(null, [ctr("cont1", runAsGroup(12))], null), "parameters": group_runasany }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_run_as_rule_any_root_user {
  input := { "review": review(null, [ctr("cont1", runAsGroup(0))], null), "parameters": group_runasany }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_run_in_range_user_in_range {
  input := { "review": review(null, [ctr("cont1", runAsGroup(150))], null), "parameters": group_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_run_in_range_user_out_of_range {
  input := { "review": review(null, [ctr("cont1", runAsGroup(10))], null), "parameters": group_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_group_one_container_run_in_range_user_lower_edge_of_range {
  input := { "review": review(null, [ctr("cont1", runAsGroup(100))], null), "parameters": group_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_run_in_range_user_upper_edge_of_range {
  input := { "review": review(null, [ctr("cont1", runAsGroup(200))], null), "parameters": group_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_run_in_range_user_between_ranges {
  input := { "review": review(null, [ctr("cont1", runAsGroup(200))], null), "parameters": group_mustrunas_two_ranges }
  results := violation with input as input
  count(results) == 1
}
test_group_two_containers_run_as_rule_any {
  input := {
    "review": review(null, [ctr("cont1", runAsGroup(1)), ctr("cont2", runAsGroup(100))], null),
    "parameters": group_runasany
  }
  results := violation with input as input
  count(results) == 0
}
test_group_two_containers_run_in_range_both_in_range {
  input := {
    "review": review(null, [ctr("cont1", runAsGroup(150)), ctr("cont2", runAsGroup(103))], null),
    "parameters": group_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 0
}
test_group_two_containers_run_in_range_one_in_range {
  input := {
    "review": review(null, [ctr("cont1", runAsGroup(150)), ctr("cont2", runAsGroup(13))], null),
    "parameters": group_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 1
}
test_group_two_containers_run_in_range_neither_in_range_two_ranges {
  input := {
    "review": review(null, [ctr("cont1", runAsGroup(150)), ctr("cont2", runAsGroup(130))], null),
    "parameters": group_mustrunas_two_ranges
  }
  results := violation with input as input
  count(results) == 2
}
test_group_one_container_one_initcontainer_run_in_range_user_in_range {
  input := {
    "review": review(null, [ctr("cont1", runAsGroup(150))], [ctr("init1", runAsGroup(150))]),
    "parameters": group_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_one_initcontainer_run_in_range_user_not_in_range {
  input := {
    "review": review(null, [ctr("cont1", runAsGroup(150))], [ctr("init1", runAsGroup(250))]),
    "parameters": group_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 1
}
test_group_one_container_empty_security_context {
  input := { "review": review(null, [ctr("cont1", {})], null), "parameters": group_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_group_one_container_no_security_context {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": group_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_group_one_container_no_security_context_RunAsAny {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": group_runasany }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_empty_security_context_empty_pod_security_context {
  input := { "review": review({}, [ctr("cont1", {})], null), "parameters": group_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_group_one_container_no_security_context_no_pod_security_context {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": group_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_group_one_container_pod_defined_null_container_run_in_range_user_in_range {
  input := { "review": review(runAsGroup(150), [ctr("cont1", null)], null), "parameters": group_mustrunas_100_200}
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_pod_defined_run_in_range_user_in_range {
  input := { "review": review(runAsGroup(150), [ctr("cont1", {})], null), "parameters": group_mustrunas_100_200}
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_pod_defined_run_in_range_user_not_in_range {
  input := { "review": review(runAsGroup(250), [ctr("cont1", {})], null), "parameters": group_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_group_one_container_pod_defined_run_in_range_container_overrides_user_in_range {
  input := { "review": review(runAsGroup(250), [ctr("cont1", runAsGroup(150))], null), "parameters": group_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_pod_defined_run_in_range_container_overrides_user_not_in_range {
  input := { "review": review(runAsGroup(150), [ctr("cont1", runAsGroup(250))], null), "parameters": group_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_group_input_container_run_as_rule_any_ignore_ranges {
  input := {
    "review": review(null, [ctr("cont1", runAsGroup(10))], null),
    "parameters": runAsGroup(rule("RunAsAny", [range(100, 200)]))
  }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_empty_security_context_mayrun {
  input := { "review": review(null, [ctr("cont1", {})], null), "parameters": group_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_no_security_context_mayrun {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": group_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_run_in_range_user_in_range_mayrun {
  input := { "review": review(null, [ctr("cont1", runAsGroup(150))], null), "parameters": group_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_run_in_range_user_out_of_range_mayrun {
  input := { "review": review(null, [ctr("cont1", runAsGroup(10))], null), "parameters": group_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_group_one_container_run_in_range_user_between_ranges_mayrun {
  input := { "review": review(null, [ctr("cont1", runAsGroup(200))], null), "parameters": group_mayrunas_two_ranges }
  results := violation with input as input
  count(results) == 1
}
test_group_two_containers_run_in_range_neither_in_range_two_ranges_mayrun {
  input := {
    "review": review(null, [ctr("cont1", runAsGroup(150)), ctr("cont2", runAsGroup(130))], null),
    "parameters": group_mayrunas_two_ranges
  }
  results := violation with input as input
  count(results) == 2
}
test_group_one_container_pod_defined_run_in_range_container_overrides_user_in_range_mayrun {
  input := { "review": review(runAsGroup(250), [ctr("cont1", runAsGroup(150))], null), "parameters": group_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_group_one_container_pod_defined_run_in_range_container_overrides_user_not_in_range_mayrun {
  input := { "review": review(runAsGroup(150), [ctr("cont1", runAsGroup(250))], null), "parameters": group_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}



## supplementalGroups ##
test_supplemental_two_containers_run_as_rule_any {
  input := {
    "review": review(supplementalGroups([15]), [ctr("cont1", null), ctr("cont2", null)], null),
    "parameters": supplemental_runasany
  }
  results := violation with input as input
  count(results) == 0
}
test_supplemental_two_containers_run_in_range_both_in_range {
  input := {
    "review": review(supplementalGroups([150]), [ctr("cont1", null), ctr("cont2", null)], null),
    "parameters": supplemental_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 0
}
test_supplemental_two_containers_run_in_range_neither_in_range_two_ranges {
  input := {
    "review": review(supplementalGroups([150]), [ctr("cont1", null), ctr("cont2", null)], null),
    "parameters": supplemental_mustrunas_two_ranges
  }
  results := violation with input as input
  count(results) == 2
}
test_supplemental_one_container_one_initcontainer_run_in_range_user_in_range {
  input := {
    "review": review(supplementalGroups([150]), [ctr("cont1", null)], [ctr("init1", null)]),
    "parameters": supplemental_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 0
}
test_supplemental_one_container_one_initcontainer_run_in_range_user_not_in_range {
  input := {
    "review": review(supplementalGroups([250]), [ctr("cont1", null)], [ctr("init1", null)]),
    "parameters": supplemental_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 2
}
test_supplemental_one_container_empty_security_context {
  input := { "review": review(null, [ctr("cont1", {})], null), "parameters": supplemental_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_supplemental_one_container_no_security_context {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": supplemental_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_supplemental_one_container_no_security_context_RunAsAny {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": supplemental_runasany }
  results := violation with input as input
  count(results) == 0
}
test_supplemental_one_container_empty_security_context_empty_pod_security_context {
  input := { "review": review({}, [ctr("cont1", {})], null), "parameters": supplemental_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_supplemental_one_container_no_security_context_no_pod_security_context {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": supplemental_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_supplemental_one_container_pod_defined_null_container_run_in_range_user_in_range {
  input := { "review": review(supplementalGroups([150]), [ctr("cont1", null)], null), "parameters": supplemental_mustrunas_100_200}
  results := violation with input as input
  count(results) == 0
}
test_supplemental_one_container_pod_defined_run_in_range_user_in_range {
  input := { "review": review(supplementalGroups([150]), [ctr("cont1", {})], null), "parameters": supplemental_mustrunas_100_200}
  results := violation with input as input
  count(results) == 0
}
test_supplemental_one_container_pod_defined_run_in_range_user_not_in_range {
  input := { "review": review(supplementalGroups([250]), [ctr("cont1", {})], null), "parameters": supplemental_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_supplemental_one_container_pod_defined_run_in_range_container_overrides_user_in_range {
  input := { "review": review(supplementalGroups([250]), [ctr("cont1", supplementalGroups(150))], null), "parameters": supplemental_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_supplemental_one_container_pod_defined_run_in_range_container_overrides_user_not_in_range {
  input := { "review": review(supplementalGroups([150]), [ctr("cont1", supplementalGroups(250))], null), "parameters": supplemental_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_supplemental_one_container_empty_security_context_mayrun {
  input := { "review": review(null, [ctr("cont1", {})], null), "parameters": supplemental_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_supplemental_one_container_no_security_context_mayrun {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": supplemental_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_supplemental_two_containers_run_in_range_neither_in_range_mayrun {
  input := {
    "review": review(supplementalGroups([230]), [ctr("cont1", null), ctr("cont2", null)], null),
    "parameters": supplemental_mayrunas_two_ranges
  }
  results := violation with input as input
  count(results) == 2
}
test_supplemental_two_containers_run_in_range_both_in_range_mayrun {
  input := {
    "review": review(supplementalGroups([100]), [ctr("cont1", null), ctr("cont2", null)], null),
    "parameters": supplemental_mayrunas_two_ranges
  }
  results := violation with input as input
  count(results) == 0
}
test_supplemental_one_container_run_in_range_two_ranges_both_in_range {
  input := { "review": review(supplementalGroups([120, 180]), [ctr("cont1", null)], null), "parameters": supplemental_mustrunas_100_200}
  results := violation with input as input
  count(results) == 0
}
test_supplemental_one_container_run_in_range_two_ranges_none_in_range {
  input := { "review": review(supplementalGroups([120, 180]), [ctr("cont1", null)], null), "parameters": supplemental_mustrunas_100_200}
  results := violation with input as input
  count(results) == 0
}
test_supplemental_one_container_run_in_range_two_ranges_one_in_range {
  input := { "review": review(supplementalGroups([150, 250]), [ctr("cont1", null)], null), "parameters": supplemental_mustrunas_100_200}
  results := violation with input as input
  count(results) == 1
}



## fsGroup ##
test_fsgroup_two_containers_run_as_rule_any {
  input := {
    "review": review(fsGroup(15), [ctr("cont1", null), ctr("cont2", null)], null),
    "parameters": fsgroup_runasany
  }
  results := violation with input as input
  count(results) == 0
}
test_fsgroup_two_containers_run_in_range_both_in_range {
  input := {
    "review": review(fsGroup(150), [ctr("cont1", null), ctr("cont2", null)], null),
    "parameters": fsgroup_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 0
}
test_fsgroup_two_containers_run_in_range_neither_in_range_two_ranges {
  input := {
    "review": review(fsGroup(150), [ctr("cont1", null), ctr("cont2", null)], null),
    "parameters": fsgroup_mustrunas_two_ranges
  }
  results := violation with input as input
  count(results) == 2
}
test_fsgroup_one_container_one_initcontainer_run_in_range_user_in_range {
  input := {
    "review": review(fsGroup(150), [ctr("cont1", null)], [ctr("init1", null)]),
    "parameters": fsgroup_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 0
}
test_fsgroup_one_container_one_initcontainer_run_in_range_user_not_in_range {
  input := {
    "review": review(fsGroup(250), null, [ctr("init1", null)]),
    "parameters": fsgroup_mustrunas_100_200
  }
  results := violation with input as input
  count(results) == 1
}
test_fsgroup_one_container_empty_security_context {
  input := { "review": review(null, [ctr("cont1", {})], null), "parameters": fsgroup_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_fsgroup_one_container_no_security_context {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": fsgroup_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_fsgroup_one_container_no_security_context_RunAsAny {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": fsgroup_runasany }
  results := violation with input as input
  count(results) == 0
}
test_fsgroup_one_container_empty_security_context_empty_pod_security_context {
  input := { "review": review({}, [ctr("cont1", {})], null), "parameters": fsgroup_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_fsgroup_one_container_no_security_context_no_pod_security_context {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": fsgroup_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_fsgroup_one_container_pod_defined_null_container_run_in_range_user_in_range {
  input := { "review": review(fsGroup(150), [ctr("cont1", null)], null), "parameters": fsgroup_mustrunas_100_200}
  results := violation with input as input
  count(results) == 0
}
test_fsgroup_one_container_pod_defined_run_in_range_user_in_range {
  input := { "review": review(fsGroup(150), [ctr("cont1", {})], null), "parameters": fsgroup_mustrunas_100_200}
  results := violation with input as input
  count(results) == 0
}
test_fsgroup_one_container_pod_defined_run_in_range_user_not_in_range {
  input := { "review": review(fsGroup(250), [ctr("cont1", {})], null), "parameters": fsgroup_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_fsgroup_one_container_pod_defined_run_in_range_container_overrides_user_in_range {
  input := { "review": review(fsGroup(250), [ctr("cont1", fsGroup(150))], null), "parameters": fsgroup_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_fsgroup_one_container_pod_defined_run_in_range_container_overrides_user_not_in_range {
  input := { "review": review(fsGroup(150), [ctr("cont1", fsGroup(250))], null), "parameters": fsgroup_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_fsgroup_input_container_run_as_rule_any_ignore_ranges {
  input := {
    "review": review(null, [ctr("cont1", fsGroup(10))], null),
    "parameters": fsGroup(rule("RunAsAny", [range(100, 200)]))
  }
  results := violation with input as input
  count(results) == 0
}
test_fsgroup_one_container_empty_security_context_mayrun {
  input := { "review": review(null, [ctr("cont1", {})], null), "parameters": fsgroup_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_fsgroup_one_container_no_security_context_mayrun {
  input := { "review": review(null, [ctr("cont1", null)], null), "parameters": fsgroup_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}



## Mixed Functionality ##
test_mixed_runAsAny_all_non_root {
  input := {"review": review(run_as_rule(12, 30, [50], 101), [ctr("cont1", null)], null), "parameters": mixed_runasany }
  results := violation with input as input
  count(results) == 0
}
test_mixed_runAsAny_all_root {
  input := {"review": review(run_as_rule(0, 0, [0], 0), [ctr("cont1", null)], null), "parameters": mixed_runasany }
  results := violation with input as input
  count(results) == 0
}
test_mixed_pod_level_all_defined_all_in_range_mustrun {
  input := {"review": review(run_as_rule(150, 150, [150], 150), [ctr("cont1", null)], null), "parameters": mixed_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_mixed_pod_level_all_defined_none_in_range_mustrun {
  input := {"review": review(run_as_rule(250, 250, [250], 250), [ctr("cont1", null)], null), "parameters": mixed_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 4
}
test_mixed_pod_level_all_defined_mixed_in_range_mustrun {
  input := {"review": review(run_as_rule(150, 150, [250], 250), [ctr("cont1", null)], null), "parameters": mixed_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 2
}
test_mixed_container_level_all_defined_all_in_range_mustrun {
  input := {"review": review(null, [ctr("cont1", run_as_rule(150, 150, [150], 150))], null), "parameters": mixed_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_mixed_container_level_all_defined_none_in_range_mustrun {
  input := {"review": review(null, [ctr("cont1", run_as_rule(250, 250, [250], 250))], null), "parameters": mixed_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 4
}
test_mixed_container_level_all_defined_mixed_in_range_mustrun {
  input := {"review": review(null, [ctr("cont1", run_as_rule(150, 150, [250], 250))], null), "parameters": mixed_mustrunas_100_200 }
  results := violation with input as input
  count(results) == 2
}
test_mixed_pod_level_all_defined_none_in_range_mayrun {
  input := {"review": review(run_as_rule(250, 250, [250], 250), [ctr("cont1", null)], null), "parameters": mixed_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 3
}
test_mixed_pod_level_all_defined_mixed_in_range_mayrun {
  input := {"review": review(run_as_rule(150, 150, [250], 250), [ctr("cont1", null)], null), "parameters": mixed_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 2
}
test_mixed_container_level_all_defined_all_in_range_mayrun {
  input := {"review": review(null, [ctr("cont1", run_as_rule(150, 150, null, null))], null), "parameters": mixed_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 0
}
test_mixed_container_level_all_defined_none_in_range_mayrun {
  input := {"review": review(null, [ctr("cont1", run_as_rule(250, 250, null, null))], null), "parameters": mixed_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_mixed_container_level_all_defined_mixed_in_range_mayrun {
  input := {"review": review(null, [ctr("cont1", run_as_rule(150, 250, null, null))], null), "parameters": mixed_mayrunas_100_200 }
  results := violation with input as input
  count(results) == 1
}
test_mixed_pod_level_all_defined_none_in_range_mixed_rules {
  input := {"review": review(run_as_rule(0, 0, 250, 250), [ctr("cont1", null)], null), "parameters": mixed_all_rules }
  results := violation with input as input
  count(results) == 3
}
# violation from  MustRunAs SupplementalGroups and MayRunAs FSGroup, range within 100&200
test_mixed_pod_level_all_defined_mixed_in_range_mixed_rules {
  input := {"review": review(run_as_rule(150, 150, 250, 250), [ctr("cont1", null)], null), "parameters": mixed_all_rules }
  results := violation with input as input
  count(results) == 2
}
# violation from no MustRunAs SupplementalGroups defined (can't define on container level)
test_mixed_container_level_all_defined_all_in_range_mixed_rules {
  input := {"review": review(null, [ctr("cont1", run_as_rule(150, 150, null, null))], null), "parameters": mixed_all_rules }
  results := violation with input as input
  count(results) == 1
}
# violation from MustRunAsNonRoot RunAsUser rule, and no MustRunAs SupplementalGroups defined (can't define on container level)
test_mixed_container_level_all_defined_none_in_range_mixed_rules {
  input := {"review": review(null, [ctr("cont1", run_as_rule(0, 0, null, null))], null), "parameters": mixed_all_rules }
  results := violation with input as input
  count(results) == 2
}
# violation from no MustRunAs SupplementalGroups defined (can't define on container level)
test_mixed_container_level_all_defined_mixed_in_range_mixed_rules {
  input := {"review": review(null, [ctr("cont1", run_as_rule(150, 150, null, null))], null), "parameters": mixed_all_rules }
  results := violation with input as input
  count(results) == 1
}





## Functions ##
review(context, containers, initContainers) = out {
  sec_obj := obj_if_exists("securityContext", context)
  cont_obj := obj_if_exists("containers", containers)
  init_obj := obj_if_exists("initContainers", initContainers)
  out = {
    "kind": {
      "kind": "Pod"
    },
    "metadata": {
      "name": "test-pod"
    },
    "object": {
      "spec": object.union(object.union(sec_obj, cont_obj), init_obj)
    }
  }
}

ctr(name, context) = out {
  name_obj := { "name": name }
  sec := obj_if_exists("securityContext", context)
  out = object.union(name_obj, sec)
}

runAsUser(user) = out {
	out = run_as_rule(user, null, null, null)
}
runAsGroup(group) = out {
	out = run_as_rule(null, group, null, null)
}
supplementalGroups(supplemental) = out {
	out = run_as_rule(null, null, supplemental, null)
}
fsGroup(fsgroup) = out {
	out = run_as_rule(null, null, null, fsgroup)
}
run_as_rule(user, group, supplemental, fsgroup) = out {
  user_obj := obj_if_exists("runAsUser", user)
  group_obj := obj_if_exists("runAsGroup", group)
  supplemental_obj := obj_if_exists("supplementalGroups", supplemental)
  fsgroup_obj := obj_if_exists("fsGroup", fsgroup)
  out = object.union(object.union(user_obj, group_obj), object.union(supplemental_obj, fsgroup_obj))
}

obj_if_exists(key, val) = out {
 not is_null(val)
 out := { key: val }
}
obj_if_exists(key, val) = out {
 is_null(val)
 out := {}
}

# Parameters
user_runasany = runAsUser(rule("RunAsAny", null))
user_mustrunas_100_200 = runAsUser(rule("MustRunAs", [range(100, 200)]))
user_mustrunas_two_ranges = runAsUser(rule("MustRunAs", [range(100, 100), range(250, 250)]))
user_mustrunasnonroot = runAsUser(rule("MustRunAsNonRoot", null))

group_runasany = runAsGroup(rule("RunAsAny", null))
group_mustrunas_100_200 = runAsGroup(rule("MustRunAs", [range(100, 200)]))
group_mustrunas_two_ranges = runAsGroup(rule("MustRunAs", [range(100, 100), range(250, 250)]))
group_mayrunas_100_200 = runAsGroup(rule("MayRunAs", [range(100, 200)]))
group_mayrunas_two_ranges = runAsGroup(rule("MayRunAs", [range(100, 100), range(250, 250)]))

supplemental_runasany = supplementalGroups(rule("RunAsAny", null))
supplemental_mustrunas_100_200 = supplementalGroups(rule("MustRunAs", [range(100, 200)]))
supplemental_mustrunas_two_ranges = supplementalGroups(rule("MustRunAs", [range(100, 100), range(250, 250)]))
supplemental_mayrunas_100_200 = supplementalGroups(rule("MayRunAs", [range(100, 200)]))
supplemental_mayrunas_two_ranges = supplementalGroups(rule("MayRunAs", [range(100, 100), range(250, 250)]))

fsgroup_runasany = fsGroup(rule("RunAsAny", null))
fsgroup_mustrunas_100_200 = fsGroup(rule("MustRunAs", [range(100, 200)]))
fsgroup_mustrunas_two_ranges = fsGroup(rule("MustRunAs", [range(100, 100), range(250, 250)]))
fsgroup_mayrunas_100_200 = fsGroup(rule("MayRunAs", [range(100, 200)]))
fsgroup_mayrunas_two_ranges = fsGroup(rule("MayRunAs", [range(100, 100), range(250, 250)]))

mixed_runasany = run_as_rule(rule("RunAsAny", null), rule("RunAsAny", null), rule("RunAsAny", null), rule("RunAsAny", null))
mixed_mustrunas_100_200 = run_as_rule(rule("MustRunAs", [range(100, 200)]), rule("MustRunAs", [range(100, 200)]), rule("MustRunAs", [range(100, 200)]), rule("MustRunAs", [range(100, 200)]))
mixed_mayrunas_100_200 = run_as_rule(rule("MustRunAsNonRoot", null), rule("MayRunAs", [range(100, 200)]), rule("MayRunAs", [range(100, 200)]), rule("MayRunAs", [range(100, 200)]))
mixed_all_rules = run_as_rule(rule("MustRunAsNonRoot", null), rule("RunAsAny", null), rule("MustRunAs", [range(100, 200)]), rule("MayRunAs", [range(100, 200)]))


rule(rule, ranges) = out {
  ranges_obj = obj_if_exists("ranges", ranges)
  out := object.union({"rule": rule}, ranges_obj)
}

range(min, max) = out {
  out := {
    "min": min,
    "max": max
  }
}
