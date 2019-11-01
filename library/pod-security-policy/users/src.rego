package k8spspallowedusers

violation[{"msg": msg}] {
  rule := input.parameters.runAsUser.rule
  input_containers[input_container]
  provided_user := run_as_user(input_container.securityContext, input.review)
  not accept_users(rule, provided_user)
  msg := sprintf("Container %v is attempting to run as disallowed user %v", [input_container.name, provided_user])
}

violation[{"msg": msg}] {
  rule := input.parameters.runAsUser.rule
  input_containers[input_container]
  not run_as_user(input_container.securityContext, input.review)
  rule != "RunAsAny"
  msg := sprintf("Container %v is attempting to run without a required securityContext/runAsUser", [input_container.name])
}

accept_users("RunAsAny", provided_user) {true}

accept_users("MustRunAsNonRoot", provided_user) = res {res := provided_user != 0}

accept_users("MustRunAs", provided_user) = res  {
  ranges := input.parameters.runAsUser.ranges
  matching := {1 | provided_user >= ranges[j].min; provided_user <= ranges[j].max}
  res := count(matching) > 0
}

input_containers[c] {
  c := input.review.object.spec.containers[_]
}

input_containers[c] {
  c := input.review.object.spec.initContainers[_]
}

run_as_user(container_security_context, review) = run_as_user {
  run_as_user := container_security_context.runAsUser
}

run_as_user(container_security_context, review) = run_as_user {
  not container_security_context.runAsUser
  review.kind.kind == "Pod"
  run_as_user := review.object.spec.securityContext.runAsUser
}
