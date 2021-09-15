package mutation

import (
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	annotationMutations  = "gatekeeper.sh/mutations"
	annotationMutationID = "gatekeeper.sh/mutation-id"
)

func mutationAnnotations(obj *unstructured.Unstructured, allAppliedMutations [][]types.Mutator, mutationUUID uuid.UUID) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[annotationMutations] = toAnnotationMutationsValue(allAppliedMutations)
	annotations[annotationMutationID] = mutationUUID.String()
	obj.SetAnnotations(annotations)
}

func toAnnotationMutationsValue(allAppliedMutations [][]types.Mutator) string {
	mutatorStringSet := make(map[string]struct{})
	for _, mutationsForIteration := range allAppliedMutations {
		for _, mutator := range mutationsForIteration {
			mutatorStringSet[mutator.String()] = struct{}{}
		}
	}

	var mutatorStrings []string
	for mutatorString := range mutatorStringSet {
		mutatorStrings = append(mutatorStrings, mutatorString)
	}
	sort.Strings(mutatorStrings)

	return strings.Join(mutatorStrings, ", ")
}
