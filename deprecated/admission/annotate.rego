package admission

import data.k8s.matches

##############################################################################
#
# Policy : Construct JSON Patch for annotating objects
#
##############################################################################

deny[{
    "id": "conditional-annotation",
    "resource": {"kind": kind, "namespace": namespace, "name": name},
    "resolution": {"patches":  p, "message" : "conditional annotation"},
}] {
    matches[[kind, namespace, name, matched_object]]
    matched_object.metadata.annotations["test-mutation"]
    p = [{"op": "add", "path": "/metadata/annotations/foo", "value": "bar"}]
}