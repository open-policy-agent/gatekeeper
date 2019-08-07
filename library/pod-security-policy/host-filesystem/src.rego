package k8spsphostfilesystem

violation[{"msg": msg, "details": {}}] {
    volume := input_hostpath_volumes[_]
    not input_hostpath_allowed(volume)
    msg := sprintf("HostPath volume %v is not allowed, pod: %v. Allowed path: %v", [volume, input.review.object.metadata.name, input.parameters.allowedHostPaths])
}

input_hostpath_allowed(volume) {
    # An empty list means there is no restriction on host paths used
    input.parameters.allowedHostPaths == []
}

input_hostpath_allowed(volume) {
    allowedHostPath := input.parameters.allowedHostPaths[_]
    path_matches(allowedHostPath.pathPrefix, volume.hostPath.path)
    not allowedHostPath.readOnly == true
}

input_hostpath_allowed(volume) {
    allowedHostPath := input.parameters.allowedHostPaths[_]
    path_matches(allowedHostPath.pathPrefix, volume.hostPath.path)
    allowedHostPath.readOnly
    not writeable_input_volume_mounts(volume.name)
}

writeable_input_volume_mounts(volume_name) {
    container := input_containers[_]
    mount := container.volumeMounts[_]
    mount.name == volume_name
    not mount.readOnly
}

# This allows "/foo", "/foo/", "/foo/bar" etc., but
# disallows "/fool", "/etc/foo" etc.
path_matches(prefix, path) {
    a := split(trim(prefix, "/"), "/")
    b := split(trim(path, "/"), "/")
    prefix_matches(a, b)
}
prefix_matches(a, b) {
    count(a) <= count(b)
    not any_not_equal_upto(a, b, count(a))
}

any_not_equal_upto(a, b, n) {
    a[i] != b[i]
    i < n
}

input_hostpath_volumes[v] {
    v := input.review.object.spec.volumes[_]
    has_field(v, "hostPath")
}

# has_field returns whether an object has a field
has_field(object, field) = true {
    object[field]
}
input_containers[c] {
    c := input.review.object.spec.containers[_]
}

input_containers[c] {
    c := input.review.object.spec.initContainers[_]
}
