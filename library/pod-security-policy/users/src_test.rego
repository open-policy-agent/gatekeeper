package allowedusersconstraint

# fails on mutant `accept_users("RunAsAny", provided_user) {false}`
test_one_container_run_as_any {
  not deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 12}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "RunAsAny"}}}}}
}

# fails on mutant `accept_users("RunAsAny", provided_user) {false}`
test_one_container_run_as_any_root_user {
  not deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 0}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "RunAsAny"}}}}}
}

test_one_container_run_as_non_root_user_is_not_root {
  not deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 1}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "MustRunAsNonRoot"}}}}}
}


test_one_container_run_as_non_root_user_is_root {
  deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 0}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "MustRunAsNonRoot"}}}}}
}

test_one_container_run_in_range_user_in_range {
  not deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 150}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "MustRunAs",
                        "ranges": [{"min": 100, "max": 200}]}}}}}
}

test_one_container_run_in_range_user_out_of_range {
  deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 10}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "MustRunAs",
                        "ranges": [{"min": 100, "max": 200}]}}}}}
}

test_one_container_run_in_range_user_lower_edge_of_range {
  not deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 100}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "MustRunAs",
                        "ranges": [{"min": 100, "max": 200}]}}}}}
}

test_one_container_run_in_range_user_upper_edge_of_range {
  not deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 200}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "MustRunAs",
                        "ranges": [{"min": 100, "max": 200}]}}}}}
}

test_one_container_run_in_range_user_between_ranges {
  deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 200}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "MustRunAs",
                        "ranges": [{"min": 100, "max": 100}, {"min": 250, "max": 250}]}}}}}
}

test_two_containers_run_as_any {
  not deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 1}},
                           {"name": "container2",
                             "securityContext": {"runAsUser": 100}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "RunAsAny"}}}}}
}

test_two_containers_run_as_non_root_users_are_not_root {
  not deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 1}},
                           {"name": "container2",
                             "securityContext": {"runAsUser": 100}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "MustRunAsNonRoot"}}}}}
}

test_two_containers_run_as_non_root_one_is_root {
  deny with input as {"review":
                     {"object": {"spec": {"containers":
                         [{"name": "container1",
                           "securityContext": {"runAsUser": 1}},
                           {"name": "container2",
                             "securityContext": {"runAsUser": 0}}]}}},
                    "constraint":
                      {"spec": {"parameters": {"runAsUser": {
                        "rule": "MustRunAsNonRoot"}}}}}
}

test_two_containers_run_in_range_both_in_range {
  not deny with input as {"review":
                   {"object": {"spec": {"containers":
                       [{"name": "container1",
                         "securityContext": {"runAsUser": 150}},
                        {"name": "container2",
                         "securityContext": {"runAsUser": 103}}]}}},
                  "constraint":
                    {"spec": {"parameters": {"runAsUser": {
                      "rule": "MustRunAs",
                      "ranges": [{"min": 100, "max": 200}]}}}}}
}

test_two_containers_run_in_range_one_in_range {
  deny with input as {"review":
                   {"object": {"spec": {"containers":
                       [{"name": "container1",
                         "securityContext": {"runAsUser": 150}},
                        {"name": "container2",
                         "securityContext": {"runAsUser": 13}}]}}},
                  "constraint":
                    {"spec": {"parameters": {"runAsUser": {
                      "rule": "MustRunAs",
                      "ranges": [{"min": 100, "max": 200}]}}}}}
}

test_two_containers_run_in_range_neither_in_range {
  deny with input as {"review":
                   {"object": {"spec": {"containers":
                       [{"name": "container1",
                         "securityContext": {"runAsUser": 150}},
                        {"name": "container2",
                         "securityContext": {"runAsUser": 130}}]}}},
                  "constraint":
                    {"spec": {"parameters": {"runAsUser": {
                      "rule": "MustRunAs",
                      "ranges": [{"min": 100, "max": 100}, {"min": 250, "max": 250}]}}}}}
}
