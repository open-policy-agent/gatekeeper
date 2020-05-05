package k8spspapparmor

test_input_apparmor_allowed_empty {
    input := { "review": input_review_container, "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 1
}

test_input_apparmor_not_allowed_no_annotation_empty {
    input := { "review": input_review_no_annotation, "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 1
}

test_input_apparmor_not_allowed_no_annotation {
    input := { "review": input_review_no_annotation, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_apparmor_container_allowed_in_list {
    input := { "review": input_review_container, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_apparmor_container_not_allowed_not_in_list {
    input := { "review": input_review_container, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_apparmor_containers_allowed_in_list {
    input := { "review": input_review_containers, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_apparmor_containers_not_allowed_not_in_list {
    input := { "review": input_review_containers, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 2
}

test_input_apparmor_containers_allowed_in_list_mixed_no_annotation {
    input := { "review": input_review_containers_missing_annotation, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_apparmor_containers_not_allowed_not_in_list_mixed_no_annotation {
    input := { "review": input_review_containers_missing_annotation, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 2
}

test_input_apparmor_containers_not_allowed_not_in_list_mixed {
    input := { "review": input_review_containers_mixed, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 1
}

input_review_container = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "container.apparmor.security.beta.kubernetes.io/nginx": "runtime/default"
            }
        },
        "spec": {
            "containers": [{
                "name": "nginx",
                "image": "nginx"
            }]
        }
    }
}


input_review_no_annotation = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": [{
                "name": "nginx",
                "image": "nginx"
            }]
        }
    }
}

input_review_containers = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "container.apparmor.security.beta.kubernetes.io/nginx": "runtime/default",
                "container.apparmor.security.beta.kubernetes.io/nginx2": "runtime/default"
            }
        },
        "spec": {
            "containers": two_containers
        }
    }
}

input_review_containers_missing_annotation = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "container.apparmor.security.beta.kubernetes.io/nginx": "runtime/default"
            }
        },
        "spec": {
            "containers": two_containers
        }
    }
}

input_review_containers_mixed = {
    "object": {
        "metadata": {
            "name": "nginx",
            "annotations": {
                "container.apparmor.security.beta.kubernetes.io/nginx": "runtime/default",
                "container.apparmor.security.beta.kubernetes.io/nginx2": "unconfined"
            }
        },
        "spec": {
            "containers": two_containers
        }
    }
}

two_containers = [{
    "name": "nginx",
    "image": "nginx"
},{
    "name": "nginx2",
    "image": "nginx"
}]

input_parameters_empty = {
    "allowedProfiles": []
}

input_parameters_in_list = {
    "allowedProfiles": [
        "runtime/default"
    ]
}

input_parameters_not_in_list = {
    "allowedProfiles": [
        "unconfined"
    ]
}
