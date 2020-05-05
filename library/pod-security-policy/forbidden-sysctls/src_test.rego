package k8spspforbiddensysctls

test_input_sysctls_allowed_all {
    input := { "review": input_review, "parameters": input_parameters_wildcard}
    results := violation with input as input
    count(results) > 0
}

test_input_sysctls_allowed_in_list {
    input := { "review": input_review, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) > 0
}

test_input_sysctls_allowed_not_in_list {
    input := { "review": input_review, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_sysctls_allowed_in_list_wildcard {
    input := { "review": input_review, "parameters": input_parameters_in_list_wildcard}
    results := violation with input as input
    count(results) > 0
}

test_input_sysctls_allowed_not_in_list_wildcard {
    input := { "review": input_review, "parameters": input_parameters_not_in_list_wildcard}
    results := violation with input as input
    count(results) == 0
}

test_input_sysctls_empty_allowed {
    input := { "review": input_review, "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 0
}

test_input_no_sysctls_wildcard_allowed {
    input := { "review": input_review_empty, "parameters": input_parameters_wildcard}
    results := violation with input as input
    count(results) == 0
}

input_review = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": {
                "name": "nginx",
                "image": "nginx",
            },
            "securityContext": {
                "sysctls": [
                    {
                        "name": "kernel.shm_rmid_forced",
                        "value": "0"
                    },
                    {
                        "name": "net.core.somaxconn",
                        "value": "1024"
                    }
                ]
            }
        }
    }
}

input_review_empty = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": {
                "name": "nginx",
                "image": "nginx",
            },
            "securityContext": {
            }
        }
    }
}

input_parameters_wildcard = {
    "forbiddenSysctls": [
        "*"
    ]
}

input_parameters_in_list = {
    "forbiddenSysctls": [
        "kernel.shm_rmid_forced",
        "net.core.somaxconn"
    ]
}

input_parameters_not_in_list = {
    "forbiddenSysctls": [
        "kernel.msgmax"
    ]
}

input_parameters_in_list_wildcard = {
    "forbiddenSysctls": [
        "kernel.*",
        "net.core*"
    ]
}

input_parameters_not_in_list_wildcard = {
    "forbiddenSysctls": [
        "kernel.msg*"
    ]
}

input_parameters_empty = {
    "forbiddenSysctls": []
}
