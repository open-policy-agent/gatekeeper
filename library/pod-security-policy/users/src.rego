package k8spspallowedusers

violation[{"msg": msg}] {
  rule := input.parameters.runAsUser.rule
  input_container := input.review.object.spec.containers[i]
  provided_user := input_container.securityContext.runAsUser
  not accept_users(rule, provided_user)
  msg := sprintf("Container %v at index %v is attempting to run as user %v", [input_container.name, i, provided_user])
}

accept_users("RunAsAny", provided_user) {true}

accept_users("MustRunAsNonRoot", provided_user) = res {res := provided_user != 0}

accept_users("MustRunAs", provided_user) = res  {
  ranges := input.parameters.runAsUser.ranges
  matching := {1 | provided_user >= ranges[j].min; provided_user <= ranges[j].max}
  res := count(matching) > 0
}
