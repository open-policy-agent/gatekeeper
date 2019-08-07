package k8spspflexvolumes

has_field(object, field) = true {
    object[field]
}

test_input_flexvolume_allowed_all {
    input := { "review": input_review, "parameters": input_parameters_wildcard}
    results := violation with input as input
    count(results) == 0
}
test_input_no_flexvolume_is_allowed {
    input := { "review": input_review_no_flexvolume, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}
test_input_flexvolume_is_allowed {
    input := { "review": input_review_many, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_flexvolume_not_allowed {
    input := { "review": input_review, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 1
}
test_input_flexvolume_many_not_allowed {
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
            "volumes": input_volumes
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
            "volumes": []
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
    "flexVolume": {
        "driver": "example/lvm"
    }
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
}]

input_parameters_wildcard = {
    "allowedFlexVolumes": []
}

input_parameters_in_list = {
    "allowedFlexVolumes": [
    {
        "driver": "example/lvm"
    },
    {
        "driver": "example/cifs"
    }]
}

input_parameters_not_in_list = {
    "allowedFlexVolumes": [
    {
        "driver": "example/testdriver"
    },
    {
        "driver": "example/cifs"
    }]
}
