package k8spspallowprivilegeescalationcontainer

test_input_container_not_privilege_escalation_allowed {
    input := { "review": input_review}
    results := violation with input as input
    count(results) == 0
}
test_input_container_privilege_escalation_not_allowed {
    input := { "review": input_review_priv}
    results := violation with input as input
    count(results) == 1
}
test_input_container_many_not_privilege_escalation_allowed {
    input := { "review": input_review_many}
    results := violation with input as input
    count(results) == 2
}
test_input_container_many_mixed_privilege_escalation_not_allowed {
    input := { "review": input_review_many_mixed}
    results := violation with input as input
    count(results) == 3
}
test_input_container_many_mixed_privilege_escalation_not_allowed_two {
    input := { "review": input_review_many_mixed_two}
    results := violation with input as input
    count(results) == 2
}
test_input_container_not_set_pod_level_allowed {
    input := { "review": pod_level_review(true)}
    results := violation with input as input
    count(results) == 1
}
test_input_container_not_set_pod_level_disallowed {
    input := { "review": pod_level_review(false)}
    results := violation with input as input
    trace(sprintf("%v", [input]))
    trace(sprintf("%v", [results]))
    count(results) == 0
}
test_input_container_allowed_override_pod_level_disallowed {
    input := { "review": pod_level_container_override_review(true, false)}
    results := violation with input as input
    count(results) == 1
}
test_input_container_disallowed_override_pod_level_allowed {
    input := { "review": pod_level_container_override_review(false, true)}
    results := violation with input as input
    count(results) == 0
}

input_review = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_one
        }
    }
}

input_review_priv = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_one_priv,
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
            "initContainers": input_containers_one
      }
    }
}

input_review_many_mixed = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_many,
            "initContainers": input_containers_one_priv
      }
    }
}

input_review_many_mixed_two = {
    "object": {
        "metadata": {
            "name": "nginx"
        },
        "spec": {
            "containers": input_containers_many_mixed,
            "initContainers": input_containers_one_priv
      }
    }
}

input_containers_one = [
{
    "name": "nginx",
    "image": "nginx",
    "securityContext": {
      "allowPrivilegeEscalation": false
    }
}]

input_containers_one_priv = [
{
    "name": "nginx",
    "image": "nginx",
    "securityContext": {
      "allowPrivilegeEscalation": true
    }
}]

input_containers_many = [
{
    "name": "nginx",
    "image": "nginx",
    "securityContext": {
      "allowPrivilegeEscalation": false
    }
},
{
    "name": "nginx1",
    "image": "nginx"
},
{
    "name": "nginx2",
    "image": "nginx",
    "securityContext": {
      "runAsUser": "1000"
    }
    
}]

input_containers_many_mixed = [
{
    "name": "nginx",
    "image": "nginx",
    "securityContext": {
      "allowPrivilegeEscalation": false
    }
},
{
    "name": "nginx1",
    "image": "nginx",
    "securityContext": {
      "allowPrivilegeEscalation": true
    }
}]

pod_level_review(pod_level_setting) = input_review {
  input_review := {
      "object": {
          "metadata": {
              "name": "nginx"
          },
          "spec": {
              "securityContext": {
                "allowPrivilegeEscalation": pod_level_setting
              },
              "containers": [
                            {
                                "name": "nginx",
                                "image": "nginx"
                            }]
          }
      }
  }
}

pod_level_container_override_review(container_setting, pod_level_setting) = input_review {
  input_review := {
      "object": {
          "metadata": {
              "name": "nginx"
          },
          "spec": {
              "securityContext": {
                "allowPrivilegeEscalation": pod_level_setting
              },
              "containers": [
                            {
                                "name": "nginx",
                                "image": "nginx",
                                "securityContext": {
                                  "allowPrivilegeEscalation": container_setting
                                }
                            }]
          }
      }
  }
}
