package k8spsphostfilesystem

test_input_hostpath_allowed_all {
    input := { "review": input_review, "parameters": input_parameters_wildcard}
    results := violation with input as input
    count(results) == 0
}
test_input_hostpath_allowed_readonly {
    input := { "review": input_review_many, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}
test_input_hostpath_not_allowed_writable {
    input := { "review": input_review_writable, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) = 1
}
test_input_hostpath_allowed_not_writable {
    input := { "review": input_review, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) > 0
}
test_input_hostpath_allowed_is_writable {
    input := { "review": input_review, "parameters": input_parameters_in_list_writable}
    results := violation with input as input
    count(results) == 0
}
test_input_hostpath_not_allowed {
    input := { "review": input_review, "parameters": input_parameters_not_in_list_writable}
    results := violation with input as input
    count(results) > 0
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

input_review_writable = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_writable,
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

input_containers_writable = [
{
    "name": "nginx",
    "image": "nginx",
    "volumeMounts":[
    {
        "mountPath": "/cache",
        "name": "cache-volume",
        "readOnly": false
    }]
}]

input_containers_many = [
{
    "name": "nginx",
    "image": "nginx",
    "volumeMounts":[
    {
        "mountPath": "/cache",
        "name": "cache-volume",
        "readOnly": true
    }]
},
{
    "name": "nginx2",
    "image": "nginx",
    "volumeMounts":[
    {
        "mountPath": "/cache",
        "name": "cache-volume2",
        "readOnly": true
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
     "hostPath": {
        "path": "/tmp/test"
    }
}]

input_parameters_wildcard = {
     "allowedHostPaths": []
}

input_parameters_in_list = {
    "allowedHostPaths": [
    {
        "readOnly": true,
        "pathPrefix": "/tmp"
    }]
}

input_parameters_in_list_writable = {
    "allowedHostPaths": [
    {
        "pathPrefix": "/tmp"
    },
    {
        "pathPrefix": "/foo"
    }]
}

input_parameters_not_in_list_writable = {
    "allowedHostPaths": [
    {
        "pathPrefix": "/foo"
    }]
}
