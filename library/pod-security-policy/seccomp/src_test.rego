package k8spspseccomp

test_input_seccomp_allowed_all {
    input := { "review": input_review, "parameters": input_parameters_wildcard}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_allowed_in_list {
    input := { "review": input_review, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seccomp_not_allowed_not_in_list {
    input := { "review": input_review, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) > 0
}

test_input_seccomp_not_allowed_no_annotation {
    input := { "review": input_review_no_annotation, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) > 0
}

input_review = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "seccomp.security.alpha.kubernetes.io/pod": "runtime/default"
            }
        },
        "spec": {
            "containers": {
                "name": "nginx",
                "image": "nginx"
            }
        }
    }
}

input_review_no_annotation = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": {
                "name": "nginx",
                "image": "nginx"
            }
        }
    }
}

input_parameters_wildcard = {
    "allowedProfiles": [
        "*"
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
