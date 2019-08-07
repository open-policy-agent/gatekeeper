package k8spspvolumetypes

violation[{"msg": msg, "details": {}}] {
    volume_fields := {x | input.review.object.spec.volumes[_][x]; x != "name"}
    not input_volume_type_allowed(volume_fields)
    msg := sprintf("One of the volume types %v is not allowed, pod: %v. Allowed volume types: %v", [volume_fields, input.review.object.metadata.name, input.parameters.volumes])
}

# * may be used to allow all volume types
input_volume_type_allowed(volume_fields) {
    input.parameters.volumes[_] == "*"
}

input_volume_type_allowed(volume_fields) {
    allowed_set := {x | x = input.parameters.volumes[_]}
    test := volume_fields - allowed_set
    count(test) == 0
}
