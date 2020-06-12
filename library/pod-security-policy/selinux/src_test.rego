package k8spspselinux

test_input_seLinux_options_on_container {
    input := { "review": input_review }
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_options_on_pod{
    input := { "review": input_review_pod_level }
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_options_no_security_context {
    input := { "review": input_review_no_security_context }
    results := violation with input as input
    count(results) == 0
}
test_input_seLinux_options_container_no_security_context {
    input := { "review": input_review_container_no_security_context }
    results := violation with input as input
    count(results) == 0
}
test_input_seLinux_options_pod_level_subset {
    input := { "review": input_review_pod_level_subset }
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_options_many {
    input := { "review": input_review_many }
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_options_mixed_seccontext {
    input := { "review": input_review_many_double_seccontext }
    results := violation with input as input
    count(results) == 3
}


input_review = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_one,
      }
    }
}

input_review_pod_level = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "securityContext": input_seLinuxOptions
        }
    }
}

input_review_pod_level_subset = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "securityContext": input_seLinuxOptions_subset
        }
    }
}

input_review_no_security_context = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {}
    }
}

input_review_container_no_security_context = {
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

input_review_many = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_many,
      }
    }
}

input_review_many_double_seccontext = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "securityContext": input_seLinuxOptions,
            "containers": input_containers_many,
            "initContainers": input_containers_one
      }
    }
}

input_containers_one = [
{
    "name": "nginx",
    "image": "nginx",
    "securityContext": input_seLinuxOptions
}]

input_containers_many = [
{
    "name": "nginx",
    "image": "nginx",
    "securityContext": {
        "privileged": true
    }
},
{
    "name": "nginx2",
    "image": "nginx"
},
{
    "name": "nginx3",
    "image": "nginx",
    "securityContext": input_seLinuxOptions
}]

input_seLinuxOptions = {
    "seLinuxOptions": {
        "level": "s0:c123,c456",
        "role": "object_r",
        "type": "svirt_sandbox_file_t",
        "user": "system_u"
    }
}

input_seLinuxOptions_subset = {
    "seLinuxOptions": {
        "level": "s0:c123,c456",
        "role": "object_r"
    }
}
