package k8spspvolumetypes

test_input_volume_type_allowed_all {
    input := { "review": input_review, "parameters": input_parameters_wildcard}
    results := violation with input as input
    count(results) == 0
}

test_input_volume_type_allowed_all_many_volumes {
    input := { "review": input_review_many, "parameters": input_parameters_wildcard}
    results := violation with input as input
    count(results) == 0
}
test_input_volume_type_none_allowed {
    input := { "review": input_review, "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 1
}
test_input_volume_type_none_allowed_many_volumes {
    input := { "review": input_review_many, "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 2
}
test_input_volume_type_allowed_all_no_volumes {
    input := { "review": input_review_no_volumes, "parameters": input_parameters_wildcard}
    results := violation with input as input
    count(results) == 0
}
test_input_volume_type_none_allowed_no_volumes {
    input := { "review": input_review_no_volumes, "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 0
}
test_input_volume_type_allowed_in_list_no_volumes {
    input := { "review": input_review_no_volumes, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_volume_type_allowed_in_list {
    input := { "review": input_review, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_volume_type_allowed_not_in_list {
    input := { "review": input_review, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_volume_type_allowed_in_list_many_volumes {
    input := { "review": input_review_many, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_volume_type_allowed_not_all_in_list_many_volumes {
    input := { "review": input_review_many, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 2
}

test_input_volume_type_allowed_in_list_many_volumes_mixed {
    input := { "review": input_review_many, "parameters": input_parameters_mixed}
    results := violation with input as input
    count(results) == 1
}

input_review = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers,
            "volumes": input_volumes
      }
    }
}

input_review_many = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_many,
            "volumes": input_volumes_many
      }
    }
}

input_review_no_volumes = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_no_volumes,
      }
    }
}

input_containers = [
{
    "name": "nginx",
    "image": "nginx",
    "volumeMounts":[
    {
        "mountPath": "/cache",
        "name": "cache-volume"
    }]
}]

input_containers_no_volumes = [
{
    "name": "nginx",
    "image": "nginx"
}]

input_containers_many = [
{
    "name": "nginx",
    "image": "nginx",
    "volumeMounts":[
    {
        "mountPath": "/cache",
        "name": "cache-volume"
    }]
},
{
    "name": "nginx2",
    "image": "nginx",
    "volumeMounts":[
    {
        "mountPath": "/cache",
        "name": "cache-volume2"
    }]
}]

input_volumes = [
{
    "name": "cache-volume",
    "hostPath": {
        "path": "/tmp"
    }
}]

input_volumes_many = [
{
    "name": "cache-volume",
    "hostPath": {
        "path": "/tmp"
    }
},
{
    "name": "cache-volume2",
    "emptyDir": {}
}]

input_parameters_empty = {
     "volumes": []
}

input_parameters_wildcard = {
     "volumes": [
         "*"
    ]
}

input_parameters_in_list = {
     "volumes": [
         "hostPath",
         "emptyDir"
    ]
}

input_parameters_mixed = {
     "volumes": [
         "configMap",
         "emptyDir"
    ]
}

input_parameters_not_in_list = {
     "volumes": [
         "configMap",
         "secret"
    ]
}
