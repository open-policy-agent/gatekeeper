package k8sallowedrepos

test_input_allowed_container {
    input := { "review": input_review(input_container_allowed), "parameters": {"repos": ["allowed"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_allowed_container_x2 {
    input := { "review": input_review(input_container_allowed), "parameters": {"repos": ["other", "allowed"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_allowed_dual_container {
    input := { "review": input_review(input_container_dual_allowed), "parameters": {"repos": ["allowed"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_denied_container {
    input := { "review": input_review(input_container_denied), "parameters": {"repos": ["allowed"]}}
    results := violation with input as input
    count(results) == 1
}
test_input_denied_container_x2 {
    input := { "review": input_review(input_container_denied), "parameters": {"repos": ["other", "allowed"]}}
    results := violation with input as input
    count(results) == 1
}
test_input_denied_dual_container {
    input := { "review": input_review(input_container_dual_denied), "parameters": {"repos": ["allowed"]}}
    results := violation with input as input
    count(results) == 2
}
test_input_denied_mixed_container {
    input := { "review": input_review(array.concat(input_container_allowed, input_container_denied)), "parameters": {"repos": ["allowed"]}}
    results := violation with input as input
    count(results) == 1
}

# init containers
test_input_allowed_container {
    input := { "review": input_init_review(input_container_allowed), "parameters": {"repos": ["allowed"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_allowed_container_x2 {
    input := { "review": input_init_review(input_container_allowed), "parameters": {"repos": ["other", "allowed"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_allowed_dual_container {
    input := { "review": input_init_review(input_container_dual_allowed), "parameters": {"repos": ["allowed"]}}
    results := violation with input as input
    count(results) == 0
}
test_input_denied_container {
    input := { "review": input_init_review(input_container_denied), "parameters": {"repos": ["allowed"]}}
    results := violation with input as input
    count(results) == 1
}
test_input_denied_container_x2 {
    input := { "review": input_init_review(input_container_denied), "parameters": {"repos": ["other", "allowed"]}}
    results := violation with input as input
    count(results) == 1
}
test_input_denied_dual_container {
    input := { "review": input_init_review(input_container_dual_denied), "parameters": {"repos": ["allowed"]}}
    results := violation with input as input
    count(results) == 2
}
test_input_denied_mixed_container {
    input := { "review": input_init_review(array.concat(input_container_allowed, input_container_denied)), "parameters": {"repos": ["allowed"]}}
    results := violation with input as input
    count(results) == 1
}

input_review(containers) = output {
    output = {
      "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": containers,
        }
      }
     }
}

input_init_review(containers) = output {
    output = {
      "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "initContainers": containers,
        }
      }
     }
}

input_container_allowed = [
{
    "name": "nginx",
    "image": "allowed/nginx",
}]

input_container_denied = [
{
    "name": "nginx",
    "image": "denied/nginx",
}]

input_container_dual_allowed = [
{
    "name": "nginx",
    "image": "allowed/nginx",
},
{
    "name": "other",
    "image": "allowed/other",
}]

input_container_dual_denied = [
{
    "name": "nginx",
    "image": "denied/nginx",
},
{
    "name": "other",
    "image": "denied/other",
}]
