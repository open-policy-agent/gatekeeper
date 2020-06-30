package k8spspflexvolumes

test_input_flexvolume_empty_params {
    input := { "review": input_review, "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 1
}
test_input_flexvolume_many_empty_params{
    input := { "review": input_review_many, "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 2
}
test_input_no_flexvolume_is_allowed {
    input := { "review": input_review_no_flexvolume, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_flexvolume_allowed {
    input := { "review": input_review, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 0
}
test_input_flexvolume_many_is_allowed {
    input := { "review": input_review_many, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_flexvolume_many_is_allowed_no_flexvolume {
    input := { "review": input_review_many_no_flexvolume, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_flexvolume_not_allowed {
    input := { "review": input_review, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 1
}
test_input_flexvolume_many_not_allowed {
    input := { "review": input_review_many_not_allowed, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 2
}
test_input_flexvolume_many_one_allowed {
    input := { "review": input_review_many, "parameters": input_parameter_in_list}
    results := violation with input as input
    count(results) == 1
}
test_input_flexvolume_many_mixed_allowed {
    input := { "review": input_review_many, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 1
}

input_review = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_one,
            "volumes": [{
                "name": "cache-volume",
                "flexVolume": {
                    "driver": "example/lvm"
                }
            }]
        }
    }
}

input_review_no_flexvolume = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_one,
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

input_review_many_no_flexvolume = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_many,
            "volumes": input_volumes_many_no_flexvolume
      }
    }
}

input_review_many_not_allowed = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_many,
            "volumes": input_volumes_not_allowed
      }
    }
}

input_containers_one = [{
    "name": "nginx",
    "image": "nginx",
    "volumeMounts":[
    {
        "mountPath": "/cache",
        "name": "cache-volume"
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

input_volumes_many = [
{
    "name": "cache-volume",
    "flexVolume": {
        "driver": "example/lvm"
    }
},
{
    "name": "cache-volume2",
    "flexVolume": {
        "driver": "example/cifs"
    }
},
{
    "name": "certs",
    "secret": {
        "secretName": "cert-data"
    }
}]

input_volumes_many_no_flexvolume = [
{
    "name": "certs",
    "secret": {
        "secretName": "cert-data"
    }
},
{
    "name": "data",
    "hostPath": {
        "path": "/tmp/data",
        "type": "File"
    }
}]

input_volumes_not_allowed = [
{
    "name": "cache-volume",
    "flexVolume": {
        "driver": "example/lvm"
    }
},
{
    "name": "cache-volume2",
    "flexVolume": {
        "driver": "example/other"
    }
}]

input_parameters_empty = {
    "allowedFlexVolumes": []
}

input_parameter_in_list = {
    "allowedFlexVolumes": [{"driver": "example/lvm"}]
}

input_parameters_in_list = {
    "allowedFlexVolumes": [
        {"driver": "example/lvm"},
        {"driver": "example/cifs"}
    ]
}

input_parameters_not_in_list = {
    "allowedFlexVolumes": [
        {"driver": "example/testdriver"},
        {"driver": "example/cifs"}
    ]
}
