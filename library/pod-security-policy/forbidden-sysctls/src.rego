package k8spspforbiddensysctls

violation[{"msg": msg, "details": {}}] {
    sysctls := {x | x = input.review.object.spec.securityContext.sysctls[_][name]}
    count(sysctls) > 0
    input_sysctls(sysctls)
    msg := sprintf("One of the sysctls %v is not allowed, pod: %v. Forbidden sysctls: %v", [sysctls, input.review.object.metadata.name, input.parameters.forbiddenSysctls])
}

# * may be used to forbid all sysctls
input_sysctls(sysctls) {
    input.parameters.forbiddenSysctls[_] == "*"
}

input_sysctls(sysctls) {
    forbidden_sysctls := {x | x = input.parameters.forbiddenSysctls[_]}
    test := sysctls & forbidden_sysctls
    count(test) > 0
}

input_sysctls(sysctls) {
    startswith(sysctls[_], trim(input.parameters.forbiddenSysctls[_], "*"))
}
