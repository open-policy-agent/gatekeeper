package authorization

user_system_control_plane(user) = res {
	res = re_match("^(system:kube-controller-manager|system:kube-scheduler)$", user)
}

is_system(name) = res {
	res = startswith(name, "system")
}

match_cud(verb) = res {
	res = re_match("^(create|patch|update|replace|delete|deletecollections)$", verb)
}
