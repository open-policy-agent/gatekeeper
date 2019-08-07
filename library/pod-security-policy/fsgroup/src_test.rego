package k8spspfsgroup

test_input_fsgroup_allowed_all {
    input := { "review": input_review_with_fsgroup, "parameters": input_parameters_runasany}
    results := violation with input as input
    count(results) == 0
}
test_input_no_fsgroup_allowed_all {
    input := { "review": input_review, "parameters": input_parameters_runasany}
    results := violation with input as input
    count(results) == 0
}
test_input_fsgroup_MustRunAs_allowed {
    input := { "review": input_review_with_fsgroup, "parameters": input_parameters_in_list_mustrunas}
    results := violation with input as input
    count(results) == 0
}
test_input_fsgroup_MustRunAs_not_allowed {
    input := { "review": input_review_with_fsgroup, "parameters": input_parameters_in_list_mustrunas_outofrange}
    results := violation with input as input
    count(results) > 0
}
test_input_no_fsgroup_MustRunAs_not_allowed {
    input := { "review": input_review, "parameters": input_parameters_in_list_mustrunas}
    results := violation with input as input
    count(results) > 0
}
test_input_securitycontext_no_fsgroup_MustRunAs_not_allowed {
    input := { "review": input_review_with_securitycontext_no_fsgroup, "parameters": input_parameters_in_list_mustrunas}
    results := violation with input as input
    count(results) > 0
}
test_input_fsgroup_MayRunAs_allowed {
    input := { "review": input_review_with_fsgroup, "parameters": input_parameters_in_list_mayrunas}
    results := violation with input as input
    count(results) == 0
}
test_input_fsgroup_MayRunAs_not_allowed {
    input := { "review": input_review_with_fsgroup, "parameters": input_parameters_in_list_mayrunas_outofrange}
    results := violation with input as input
    count(results) > 0
}
test_input_no_fsgroup_MayRunAs_allowed {
    input := { "review": input_review, "parameters": input_parameters_in_list_mayrunas}
    results := violation with input as input
    count(results) == 0
}
test_input_securitycontext_no_fsgroup_MayRunAs_allowed {
    input := { "review": input_review_with_securitycontext_no_fsgroup, "parameters": input_parameters_in_list_mayrunas}
    results := violation with input as input
    count(results) == 0
}

input_review = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_one,
            "volumes": input_volumes
      }
    }
}

input_review_with_fsgroup = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "securityContext": {
              "fsGroup": 2000
            },
            "containers": input_containers_one,
            "volumes": input_volumes
      }
    }
}

input_review_with_securitycontext_no_fsgroup = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "securityContext": {
              "runAsUser": "1000"
            },
            "containers": input_containers_one,
            "volumes": input_volumes
      }
    }
}

input_containers_one = [
{
    "name": "nginx",
    "image": "nginx",
    "volumeMounts":[
    {
        "mountPath": "/cache",
        "name": "cache-volume"
    }]
}]

input_volumes = [
{
    "name": "cache-volume",
    "emptyDir": {}
}]

input_parameters_runasany = {
     "rule": "RunAsAny"
}

input_parameters_in_list_mustrunas = {
    "rule": "MustRunAs",
    "ranges": [
    {
        "min": 1,
        "max": 2000
    }]
}
input_parameters_in_list_mustrunas_outofrange = {
    "rule": "MustRunAs",
    "ranges": [
    {
        "min": 1,
        "max": 1000
    }]
}
input_parameters_in_list_mayrunas = {
    "rule": "MayRunAs",
    "ranges": [
    {
        "min": 1,
        "max": 2000
    }]
}
input_parameters_in_list_mayrunas_outofrange = {
    "rule": "MayRunAs",
    "ranges": [
    {
        "min": 1,
        "max": 1000
    }]
}
