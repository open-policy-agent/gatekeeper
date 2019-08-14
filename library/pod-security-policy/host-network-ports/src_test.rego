package k8spsphostnetworkingports

test_input_no_hostnetwork_no_port_is_allowed {
    input := { "review": input_review, "parameters": input_parameters_ports}
    results := violation with input as input
    count(results) == 0
}
test_input_no_hostnetwork_allowed_ports_is_allowed {
    input := { "review": input_review_no_hostnetwork_allowed_ports, "parameters": input_parameters_ports}
    results := violation with input as input
    count(results) == 0
}
test_input_no_hostnetwork_container_ports_not_allowed {
    input := { "review": input_review_no_hostnetwork_container_ports_outofrange, "parameters": input_parameters_ports}
    results := violation with input as input
    count(results) > 0
}
test_input_with_hostnetwork_is_allowed {
    input := { "review": input_review_with_hostnetwork_no_port, "parameters": input_parameters_ports}
    results := violation with input as input
    count(results) == 0
}
test_input_with_hostnetwork_constraint_no_hostnetwork_not_allowed {
    input := { "review": input_review_with_hostnetwork_no_port, "parameters": input_parameters_ports_no_hostnetwork}
    results := violation with input as input
    count(results) > 0
}
test_input_with_hostnetwork_constraint_no_hostnetwork_explicit {
    input := { "review": input_review_no_hostnetwork_explicit, "parameters": input_parameters_ports_no_hostnetwork}
    results := violation with input as input
    count(results) == 0
}
test_input_with_hostnetwork_container_ports_not_allowed {
    input := { "review": input_review_with_hostnetwork_port_outofrange, "parameters": input_parameters_ports}
    results := violation with input as input
    count(results) == 1
}

input_review = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_one
      }
    }
}

input_review_no_hostnetwork_allowed_ports = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_one_port
      }
    }
}

input_review_no_hostnetwork_container_ports_outofrange = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_one_port_outofrange
      }
    }
}

input_review_with_hostnetwork_no_port = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "hostNetwork": true,
            "containers": input_containers_one
      }
    }
}


input_review_no_hostnetwork_explicit = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "hostNetwork": false,
            "containers": input_containers_one
      }
    }
}

input_review_with_hostnetwork_port_outofrange = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "hostNetwork": true,
            "containers": input_containers_many_port_outofrange,
            "initContainers": input_containers_one_port_outofrange
      }
    }
}
input_containers_one = [
{
    "name": "nginx",
    "image": "nginx"
}]

input_containers_one_port = [
{
    "name": "nginx",
    "image": "nginx",
    "ports": [{
      "containerPort": 80,
      "hostPort": 8080
    }]
}]
input_containers_one_port_outofrange = [
{
    "name": "nginx",
    "image": "nginx",
    "ports": [{
      "containerPort": 80,
      "hostPort": 9001
    }]
}]

input_containers_many_port_outofrange = [
{
    "name": "nginx",
    "image": "nginx",
    "ports": [{
      "containerPort": 80,
      "hostPort": 9001
    }]
},
{
    "name": "nginx1",
    "image": "nginx",
    "ports": [{
      "containerPort": 9200,
      "hostPort": 9200
    }]
}]

input_parameters_ports = {
    "hostNetwork": true,
    "min": 80,
    "max": 9000
}

input_parameters_ports_no_hostnetwork = {
    "min": 80,
    "max": 9000
}
