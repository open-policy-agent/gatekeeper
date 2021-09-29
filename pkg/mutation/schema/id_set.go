package schema

import (
	"fmt"
	"sort"
	"strings"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
)

type IDSet map[types.ID]bool

func (c IDSet) String() string {
	ids := c.ToList()
	idStrings := make([]string, len(ids))
	for i, id := range ids {
		idStrings[i] = id.String()
	}

	return fmt.Sprintf("[%s]", strings.Join(idStrings, ","))
}

func (c IDSet) ToList() []types.ID {
	result := make([]types.ID, len(c))

	idx := 0
	for id := range c {
		result[idx] = id
		idx++
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].String() < result[j].String()
	})

	return result
}
