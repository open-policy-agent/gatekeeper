package k8spsphostnamespace

test_input_no_hostnamespace_allowed {
    input := { "review": input_review}
    results := violation with input as input
    count(results) == 0
}
test_input_hostPID_not_allowed {
    input := { "review": input_review_hostPID}
    results := violation with input as input
    count(results) > 0
}
test_input_hostIPC_not_allowed {
    input := { "review": input_review_hostIPC}
    results := violation with input as input
    count(results) > 0
}
test_input_hostnamespace_both_not_allowed {
    input := { "review": input_review_hostnamespace_both}
    results := violation with input as input
    count(results) > 0
}

input_review = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers
      }
    }
}
input_review_hostPID = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "hostPID": true,
            "containers": input_containers
      }
    }
}

input_review_hostIPC = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "hostIPC": true,
            "containers": input_containers
      }
    }
}
input_review_hostnamespace_both = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {

            "hostPID": true,
            "hostIPC": true,
            "containers": input_containers
      }
    }
}
input_containers = [
{
    "name": "nginx",
    "image": "nginx"
}]
