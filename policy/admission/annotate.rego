package admission

import data.k8s.matches

##############################################################################
#
# Policy : Construct JSON Patch for annotating objects
#
##############################################################################

patch[{
    "id": "conditional-annotation",
    "resource": {"kind": kind, "namespace": namespace, "name": name},
    "patch":  p,
}] {
    matches[[kind, namespace, name, matched_object]]
    matched_object.metadata.annotations["test-mutation"]
    p = [{"op": "add", "path": "/metadata/annotations/foo", "value": "bar"}]
}