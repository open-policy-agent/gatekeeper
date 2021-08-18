package mutation

import (
	"fmt"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
)

func addCache(m types.Mutator, currentAsObject map[string]interface{}, reference string, providerResponseCache map[types.ProviderCacheKey]string, username string) {
	dataSource := m.GetExternalDataSource()
	providerName := m.GetExternalDataProvider()
	key := types.ProviderCacheKey{
		ProviderName: providerName,
		DataSource:   dataSource,
	}

	switch m.GetExternalDataSource() {
	// username from admission request userInfo
	case types.Username:
		key.OutboundData = username
	// value at location
	case types.ValueAtLocation:
		key.OutboundData = fmt.Sprintf("%v", currentAsObject[reference])
	default:
	}

	providerResponseCache[key] = ""
}

func (s *System) populateProviderCache(m types.Mutator, current interface{}, depth int, providerResponseCache map[types.ProviderCacheKey]string, username string) error {
	pathEntry := m.Path().Nodes[depth]
	switch castPathEntry := pathEntry.(type) {
	case *parser.Object:
		currentAsObject, ok := current.(map[string]interface{})
		if !ok { // Path entry type does not match current object
			return fmt.Errorf("mismatch between path entry (type: object) and received object (type: %T). Path: %+v", current, castPathEntry)
		}
		next, exists := currentAsObject[castPathEntry.Reference]

		// we have hit the end of our path, this is the base case
		if len(m.Path().Nodes)-1 == depth {
			addCache(m, currentAsObject, castPathEntry.Reference, providerResponseCache, username)
			return nil
		}
		if !exists { // Next element is missing and needs to be added
			return nil
		}

		if err := s.populateProviderCache(m, next, depth+1, providerResponseCache, username); err != nil {
			return err
		}

		// TODO(Sertac): handle this better
		if m.ID().Kind == "AssignMetadata" {
			addCache(m, currentAsObject, castPathEntry.Reference, providerResponseCache, username)
		}

		return nil
	case *parser.List:
		currentAsList, ok := current.([]interface{})
		if !ok { // Path entry type does not match current object
			return fmt.Errorf("mismatch between path entry (type: List) and received object (type: %T). Path: %+v", current, castPathEntry)
		}
		shallowCopy := make([]interface{}, len(currentAsList))
		copy(shallowCopy, currentAsList)

		glob := castPathEntry.Glob
		key := castPathEntry.KeyField

		for _, listElement := range shallowCopy {
			if glob {
				if err := s.populateProviderCache(m, listElement, depth+1, providerResponseCache, username); err != nil {
					return err
				}
			} else if listElementAsObject, ok := listElement.(map[string]interface{}); ok {
				if elementValue, ok := listElementAsObject[key]; ok {
					if castPathEntry.KeyValue == elementValue {
						if err := s.populateProviderCache(m, listElement, depth+1, providerResponseCache, username); err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("invalid type pathEntry type: %T", pathEntry)
	}
}
