package k8sazureallowedseccomp

test_input_seccomp_allowed_empty {
    input := { "review": input_review_pod_single, "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 1
}

test_input_seccomp_allowed_all {
    input := { "review": input_review_pod_single, "parameters": input_parameters_wildcard}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_allowed_in_list {
    input := { "review": input_review_pod_single, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_not_allowed_not_in_list {
    input := { "review": input_review_pod_single, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 1
}
test_input_seccomp_pod_multiple_allowed_empty {
    input := { "review": input_review_pod_multiple, "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 2
}

test_input_seccomp_pod_multiple_allowed_all {
    input := { "review": input_review_pod_multiple, "parameters": input_parameters_wildcard}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_pod_multiple_allowed_in_list {
    input := { "review": input_review_pod_multiple, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_pod_multiple_not_allowed_not_in_list {
    input := { "review": input_review_pod_multiple, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 2
}

test_input_seccomp_not_allowed_no_annotation {
    input := { "review": input_review_no_annotation, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_seccomp_not_allowed_multiple_no_annotation {
    input := { "review": input_review_containers_no_annotation, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 2
}

test_input_seccomp_container_allowed_all {
    input := { "review": input_review_container, "parameters": input_parameters_wildcard}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_container_allowed_in_list {
    input := { "review": input_review_container, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_container_not_allowed_not_in_list {
    input := { "review": input_review_container, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_seccomp_containers_allowed_in_list {
    input := { "review": input_review_containers, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_containers_not_allowed_not_in_list {
    input := { "review": input_review_containers, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 2
}

test_input_seccomp_containers_mixed {
    input := { "review": input_review_containers_mixed, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_seccomp_containers_mixed_missing_annotation {
    input := { "review": input_review_containers_missing, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_seccomp_containers_allowed_in_list_multiple {
    input := { "review": input_review_containers_mixed, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_pod_container_annotation {
    input := { "review": input_review_pod_container, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_pod_container_annotation_not_allowed {
    input := { "review": input_review_pod_container, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 2
}

test_input_seccomp_pod_container_annotation_both_allowed {
    input := { "review": input_review_pod_container_mixed, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_pod_container_annotation_mixed_allowed {
    input := { "review": input_review_pod_container_mixed, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_seccomp_pod_container_annotation_mixed_not_allowed {
    input := { "review": input_review_pod_container_mixed, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 2
}

test_input_seccomp_pod_container_annotation_both_allowed_reversed {
    input := { "review": input_review_pod_container_mixed_reversed, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_pod_container_annotation_mixed_allowed_reversed {
    input := { "review": input_review_pod_container_mixed_reversed, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_seccomp_pod_container_annotation_mixed_not_allowed_reversed {
    input := { "review": input_review_pod_container_mixed_reversed, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 2
}

#Init Containers
test_input_init_seccomp_pod_container_annotation_both_allowed {
    input := { "review": input_review_init_pod_container_mixed, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_init_seccomp_pod_container_annotation_mixed_allowed {
    input := { "review": input_review_init_pod_container_mixed, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_init_seccomp_pod_container_annotation_mixed_not_allowed {
    input := { "review": input_review_init_pod_container_mixed, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 2
}

input_review_pod_single = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "seccomp.security.alpha.kubernetes.io/pod": "runtime/default"
            }
        },
        "spec": {
            "containers": single_container
        }
    }
}

input_review_pod_multiple = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "seccomp.security.alpha.kubernetes.io/pod": "runtime/default"
            }
        },
        "spec": {
            "containers": multiple_containers
        }
    }
}

input_review_container = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "container.seccomp.security.alpha.kubernetes.io/nginx": "runtime/default"
            }
        },
        "spec": {
            "containers": single_container
        }
    }
}

input_review_containers = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "container.seccomp.security.alpha.kubernetes.io/nginx": "runtime/default",
                "container.seccomp.security.alpha.kubernetes.io/nginx2": "runtime/default"
            }
        },
        "spec": {
            "containers": multiple_containers
        }
    }
}

input_review_containers_mixed = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "container.seccomp.security.alpha.kubernetes.io/nginx": "runtime/default",
                "container.seccomp.security.alpha.kubernetes.io/nginx2": "docker/default"
            }
        },
        "spec": {
            "containers": multiple_containers
        }
    }
}

input_review_containers_missing = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "container.seccomp.security.alpha.kubernetes.io/nginx": "runtime/default"
            }
        },
        "spec": {
            "containers": multiple_containers
        }
    }
}

input_review_pod_container = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "seccomp.security.alpha.kubernetes.io/pod": "runtime/default",
                "container.seccomp.security.alpha.kubernetes.io/nginx": "runtime/default"
            }
        },
        "spec": {
            "containers": multiple_containers
        }
    }
}

input_review_pod_container_mixed = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "seccomp.security.alpha.kubernetes.io/pod": "runtime/default",
                "container.seccomp.security.alpha.kubernetes.io/nginx": "docker/default"
            }
        },
        "spec": {
            "containers": multiple_containers
        }
    }
}

input_review_pod_container_mixed_reversed = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "seccomp.security.alpha.kubernetes.io/pod": "docker/default",
                "container.seccomp.security.alpha.kubernetes.io/nginx": "runtime/default"
            }
        },
        "spec": {
            "containers": multiple_containers
        }
    }
}

input_review_init_pod_container_mixed = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "seccomp.security.alpha.kubernetes.io/pod": "runtime/default",
                "container.seccomp.security.alpha.kubernetes.io/nginx": "docker/default"
            }
        },
        "spec": {
            "initContainers": multiple_containers
        }
    }
}


input_review_no_annotation = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": single_container
        }
    }
}


input_review_containers_no_annotation = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": multiple_containers
        }
    }
}

single_container = [{
    "name": "nginx",
    "image": "nginx"
}]

multiple_containers = [{
    "name": "nginx",
    "image": "nginx"
}, {
    "name": "nginx2",
    "image": "nginx"
}]

input_parameters_empty = {
    "allowedProfiles": []
}

input_parameters_wildcard = {
    "allowedProfiles": [
        "*"
    ]
}

input_parameter_in_list = {
    "allowedProfiles": [
        "runtime/default"
    ]
}

input_parameters_in_list = {
    "allowedProfiles": [
        "runtime/default",
        "docker/default"
    ]
}

input_parameters_not_in_list = {
    "allowedProfiles": [
        "unconfined"
    ]
}
