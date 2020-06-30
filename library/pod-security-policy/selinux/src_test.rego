package k8spspselinux

test_input_seLinux_options_allowed_in_list {
    input := { "review": input_review, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seLinux_options_allowed_in_list_subset {
    input := { "review": input_review, "parameters": input_parameters_in_list_subset}
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_options_allowed_in_list_split_list {
    input := { "review": input_review, "parameters": input_parameters_in_list_split_two}
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_option_not_allowed_not_in_list {
    input := { "review": input_review, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_options_empty {
    input := { "review": input_review, "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_option_two_empty {
    input := { "review": input_review_two , "parameters": input_parameters_empty}
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_options_no_security_context {
    input := { "review": input_review_no_security_context, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seLinux_options_two_allowed_in_list {
    input := { "review": input_review_two, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seLinux_options_two_subset_allowed_in_list {
    input := { "review": input_review_two_subset, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seLinux_options_subset_allowed_in_list_subset {
    input := { "review": input_review_two_subset, "parameters": input_parameters_in_list_subset}
    results := violation with input as input
    count(results) == 0
}

test_input_seLinux_options_subset_allowed_in_list_split_subset {
    input := { "review": input_review_two_subset, "parameters": input_parameters_in_list_split_subset}
    results := violation with input as input
    count(results) == 0
}

test_input_seLinux_options_allowed_in_list_split_subset {
    input := { "review": input_review, "parameters": input_parameters_in_list_split_subset}
    results := violation with input as input
    count(results) == 1
}


test_input_seLinux_options_two_subset_not_allowed_not_in_list {
    input := { "review": input_review_two_subset, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_option_two_not_allowed_not_in_list {
    input := { "review": input_review_two, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_options_many_allowed_in_list {
    input := { "review": input_review_many, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seLinux_options_many_not_allowed_not_in_list {
    input := { "review": input_review_many, "parameters": input_parameters_not_in_list}
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_options_many_not_allowed_not_in_list_two {
    input := { "review": input_review_many, "parameters": input_parameters_not_in_list_two}
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_option_two_allowed_in_list_subset {
    input := { "review": input_review_two , "parameters": input_parameters_in_list_subset}
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_option_two_not_allowed_not_in_list_subset {
    input := { "review": input_review_two , "parameters": input_parameters_not_in_list_two}
    results := violation with input as input
    count(results) == 1
}

test_input_seLinux_options_many_allowed_in_list_double_seccontext {
    input := { "review": input_review_many_double_seccontext, "parameters": input_parameters_in_list}
    results := violation with input as input
    count(results) == 0
}

test_input_seLinux_options_many_not_allowed_not_in_list_double_seccontext {
    input := { "review": input_review_many_double_seccontext, "parameters": input_parameters_not_in_list}
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

input_review_two = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "securityContext": input_seLinuxOptions
        }
    }
}

input_review_two_subset = {
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

input_parameters_in_list = {
    "allowedSELinuxOptions": [{
        "level": "s0:c123,c456",
        "role": "object_r",
        "type": "svirt_sandbox_file_t",
        "user": "system_u"
    }]
}

input_parameters_in_list_split_two = {
    "allowedSELinuxOptions": [{
        "level": "s0:c123,c456",
        "role": "object_r",
        "type": "svirt_sandbox_file_f",
        "user": "system_v"
    }, {
        "level": "s1:c234,c567",
        "role": "object_f",
        "type": "svirt_sandbox_file_t",
        "user": "system_u"
    }]
}

input_parameters_in_list_split_subset = {
    "allowedSELinuxOptions": [{
        "level": "s0:c123,c456",
        "role": "object_r"
    }, {
        "type": "svirt_sandbox_file_t",
        "user": "system_u"
    }]
}

input_parameters_in_list_subset = {
    "allowedSELinuxOptions": [{
        "level": "s0:c123,c456",
        "role": "object_r"
    }]
}

input_parameters_not_in_list = {
    "allowedSELinuxOptions": [{
        "level": "s1:c234,c567",
        "role": "sysadm_r",
        "type": "svirt_lxc_net_t",
        "user": "sysadm_u"
    }]
}


input_parameters_not_in_list_two = {
    "allowedSELinuxOptions": [{
        "level": "s1:c234,c567"
    }, {
        "level": "s2:c345,c678"
    }]
}

input_parameters_empty = {
    "allowedSELinuxOptions": []
}
