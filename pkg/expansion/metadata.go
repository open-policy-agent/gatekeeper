package expansion

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// ensureOwnerReference appends an OwnerReference describing parent to the resultant
// resource if one is not already present.
func ensureOwnerReference(resultant, parent *unstructured.Unstructured) {
	if resultant == nil || parent == nil {
		return
	}

	parentAPIVersion := parent.GetAPIVersion()
	parentKind := parent.GetKind()
	parentName := parent.GetName()
	if parentAPIVersion == "" || parentKind == "" || parentName == "" {
		return
	}

	ownerRef := map[string]interface{}{
		"apiVersion": parentAPIVersion,
		"kind":       parentKind,
		"name":       parentName,
	}

	refs, found, err := unstructured.NestedSlice(resultant.Object, "metadata", "ownerReferences")
	if err != nil {
		return
	}
	if found {
		for _, ref := range refs {
			refMap, ok := ref.(map[string]interface{})
			if !ok {
				continue
			}
			if refMap["apiVersion"] == parentAPIVersion && refMap["kind"] == parentKind && refMap["name"] == parentName {
				return
			}
		}
	} else {
		refs = []interface{}{}
	}

	refs = append(refs, ownerRef)
	_ = unstructured.SetNestedSlice(resultant.Object, refs, "metadata", "ownerReferences")
}
