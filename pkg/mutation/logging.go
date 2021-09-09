package mutation

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/open-policy-agent/gatekeeper/pkg/logging"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func logAppliedMutations(message string, mutationUUID uuid.UUID, obj *unstructured.Unstructured, allAppliedMutations [][]types.Mutator) {
	iterations := make([]interface{}, 0, 2*len(allAppliedMutations))
	for i, appliedMutations := range allAppliedMutations {
		if len(appliedMutations) == 0 {
			continue
		}

		var appliedMutationsText []string
		for _, mutator := range appliedMutations {
			appliedMutationsText = append(appliedMutationsText, mutator.String())
		}

		iterations = append(iterations, fmt.Sprintf("iteration_%d", i), strings.Join(appliedMutationsText, ", "))
	}

	if len(iterations) > 0 {
		logDetails := []interface{}{
			"Mutation Id", mutationUUID,
			logging.EventType, logging.MutationApplied,
			logging.ResourceGroup, obj.GroupVersionKind().Group,
			logging.ResourceKind, obj.GroupVersionKind().Kind,
			logging.ResourceAPIVersion, obj.GroupVersionKind().Version,
			logging.ResourceNamespace, obj.GetNamespace(),
			logging.ResourceName, obj.GetName(),
		}
		logDetails = append(logDetails, iterations...)
		log.Info(message, logDetails...)
	}
}
