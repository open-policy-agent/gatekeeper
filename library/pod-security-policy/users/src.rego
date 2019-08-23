package allowedusersconstraint

deny {
  rule := input.constraint.spec.parameters.runAsUser.rule
  input_container := input.review.object.spec.containers[i]
  provided_user := input_container.securityContext.runAsUser

  not accept_users(rule, provided_user)
}

accept_users("RunAsAny", provided_user) {true}

accept_users("MustRunAsNonRoot", provided_user) = res {res := provided_user != 0}

accept_users("MustRunAs", provided_user) = res  {
  ranges := input.constraint.spec.parameters.runAsUser.ranges
  matching := {1 | provided_user >= ranges[j].min; provided_user <= ranges[j].max}
  res := count(matching) > 0
}
