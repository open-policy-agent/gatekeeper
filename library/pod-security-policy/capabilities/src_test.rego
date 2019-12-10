package capabilities

test_input_all_allowed {
    input := { "review": input_review([cadd(["one", "two"])]), "parameters": {"allowedCapabilities": ["*"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_all_allowed_container_x2 {
    input := { "review": input_review([cadd(["one", "two"]), cadd(["three"])]), "parameters": {"allowedCapabilities": ["*"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_one_allowed {
    input := { "review": input_review([cadd(["one"])]), "parameters": {"allowedCapabilities": ["one"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_one_allowed_container_x2 {
    input := { "review": input_review([cadd(["one"]), cadd(["one"])]), "parameters": {"allowedCapabilities": ["one"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_two_allowed_container_x2 {
    input := { "review": input_review([cadd(["one"]), cadd(["two"])]), "parameters": {"allowedCapabilities": ["one", "two"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_two_allowed_two_used_container_x2 {
    input := { "review": input_review([cadd(["one", "two"]), cadd(["one", "two"])]), "parameters": {"allowedCapabilities": ["one", "two"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_none_allowed {
    input := { "review": input_review([cadd(["one"])]), "parameters": {"allowedCapabilities": []}}
    results := violation with input as input
    count(results) == 1
}
test_input_none_allowed_undefined {
    input := { "review": input_review([cadd(["one"])]), "parameters": {}}
    results := violation with input as input
    count(results) == 1
}
test_input_none_allowed_undefined_x2_x2 {
    input := { "review": input_review([cadd(["one", "two"]), cadd(["three", "two"])]), "parameters": {}}
    results := violation with input as input
    trace(sprintf("results are: %v", [results]))
    count(results) == 2
}
test_input_disallowed_x1 {
    input := { "review": input_review([cadd(["three"])]), "parameters": {"allowedCapabilities": ["one", "two"]}}
    results := violation with input as input
    count(results) == 1
}
test_input_disallowed_x2_just_one {
    input := { "review": input_review([cadd(["one"]), cadd(["three", "two"])]), "parameters": {"allowedCapabilities": ["one", "two"]}}
    results := violation with input as input
    count(results) == 1
}
test_input_disallowed_x2 {
    input := { "review": input_review([cadd(["three"]), cadd(["three", "two"])]), "parameters": {"allowedCapabilities": ["one", "two"]}}
    results := violation with input as input
    count(results) == 2
}


test_input_empty_drop {
   input := { "review": input_review([cdrop(["one", "two"])]), "parameters": {"requiredDropCapabilities": []}}
   results := violation with input as input
   count(results) == 0
}
test_input_all_dropped {
   input := { "review": input_review([cdrop(["one", "two"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 0
}
test_input_extra_dropped {
   input := { "review": input_review([cdrop(["one", "two", "three"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 0
}
test_input_all_dropped_x2 {
   input := { "review": input_review([cdrop(["one", "two"]), cdrop(["one", "two"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 0
}
test_input_missing_drop {
   input := { "review": input_review([cdrop(["two"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 1
}
test_input_one_missing_drop_x2 {
   input := { "review": input_review([cdrop(["one"]), cdrop(["one", "two"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 1
}
test_input_missing_drop_x2 {
   input := { "review": input_review([cdrop(["one"]), cdrop(["two"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 2
}
test_input_drop_undefined_x2 {
   input := { "review": input_review([cadd([]), cadd([])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 2
}

# init containers
test_input_all_allowed {
    input := { "review": input_init_review([cadd(["one", "two"])]), "parameters": {"allowedCapabilities": ["*"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_all_allowed_container_x2 {
    input := { "review": input_init_review([cadd(["one", "two"]), cadd(["three"])]), "parameters": {"allowedCapabilities": ["*"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_one_allowed {
    input := { "review": input_init_review([cadd(["one"])]), "parameters": {"allowedCapabilities": ["one"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_one_allowed_container_x2 {
    input := { "review": input_init_review([cadd(["one"]), cadd(["one"])]), "parameters": {"allowedCapabilities": ["one"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_two_allowed_container_x2 {
    input := { "review": input_init_review([cadd(["one"]), cadd(["two"])]), "parameters": {"allowedCapabilities": ["one", "two"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_two_allowed_two_used_container_x2 {
    input := { "review": input_init_review([cadd(["one", "two"]), cadd(["one", "two"])]), "parameters": {"allowedCapabilities": ["one", "two"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_none_allowed {
    input := { "review": input_init_review([cadd(["one"])]), "parameters": {"allowedCapabilities": []}}
    results := violation with input as input
    count(results) == 1
}
test_input_none_allowed_undefined {
    input := { "review": input_init_review([cadd(["one"])]), "parameters": {}}
    results := violation with input as input
    count(results) == 1
}
test_input_none_allowed_undefined_x2_x2 {
    input := { "review": input_init_review([cadd(["one", "two"]), cadd(["three", "two"])]), "parameters": {}}
    results := violation with input as input
    count(results) == 2
}
test_input_disallowed_x1 {
    input := { "review": input_init_review([cadd(["three"])]), "parameters": {"allowedCapabilities": ["one", "two"]}}
    results := violation with input as input
    count(results) == 1
}
test_input_disallowed_x2_just_one {
    input := { "review": input_init_review([cadd(["one"]), cadd(["three", "two"])]), "parameters": {"allowedCapabilities": ["one", "two"]}}
    results := violation with input as input
    count(results) == 1
}
test_input_disallowed_x2 {
    input := { "review": input_init_review([cadd(["three"]), cadd(["three", "two"])]), "parameters": {"allowedCapabilities": ["one", "two"]}}
    results := violation with input as input
    count(results) == 2
}


test_input_empty_drop {
   input := { "review": input_init_review([cdrop(["one", "two"])]), "parameters": {"requiredDropCapabilities": []}}
   results := violation with input as input
   count(results) == 0
}
test_input_all_dropped {
   input := { "review": input_init_review([cdrop(["one", "two"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 0
}
test_input_extra_dropped {
   input := { "review": input_init_review([cdrop(["one", "two", "three"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 0
}
test_input_all_dropped_x2 {
   input := { "review": input_init_review([cdrop(["one", "two"]), cdrop(["one", "two"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 0
}
test_input_missing_drop {
   input := { "review": input_init_review([cdrop(["two"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 1
}
test_input_one_missing_drop_x2 {
   input := { "review": input_init_review([cdrop(["one"]), cdrop(["one", "two"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 1
}
test_input_missing_drop_x2 {
   input := { "review": input_init_review([cdrop(["one"]), cdrop(["two"])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 2
}
test_input_drop_undefined_x2 {
   input := { "review": input_init_review([cadd([]), cadd([])]), "parameters": {"requiredDropCapabilities": ["one", "two"]}}
   results := violation with input as input
   count(results) == 2
}

input_review(containers) = output {
    cs := [o | c := containers[i]; o := inject_name(i, c)]
    output = {
      "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": cs,
        }
      }
     }
}

input_init_review(containers) = output {
    cs := [o | c := containers[i]; o := inject_name(i, c)]
    output = {
      "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "initContainers": cs,
        }
      }
     }
}

cdrop(drop) = output {
  output := {
    "securityContext": {
     "capabilities": {
       "drop": drop
     }
    }
  }
}

cadd(add) = output {
  output := {
    "securityContext": {
     "capabilities": {
       "add": add
     }
    }
  }
}

inject_name(name, obj) = out {
  keys := {k | obj[k]}
  all_keys := keys | {"name"}
  out := {k: v | k := all_keys[_]; v:= get_default(obj, k, name)}
}

