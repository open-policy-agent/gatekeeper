package mutators

import (
	"reflect"
	"sync"
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/types"
)

func TestCache_TallyStatus(t *testing.T) {
	type fields struct {
		cache map[types.ID]mutatorStatus
	}
	tests := []struct {
		name   string
		fields fields
		want   map[MutatorIngestionStatus]int
	}{
		{
			name: "empty cache",
			fields: fields{
				cache: make(map[types.ID]mutatorStatus),
			},
			want: map[MutatorIngestionStatus]int{
				MutatorStatusActive: 0,
				MutatorStatusError:  0,
			},
		},
		{
			name: "one active mutator",
			fields: fields{
				cache: map[types.ID]mutatorStatus{
					{Name: "foo"}: {
						ingestion: MutatorStatusActive,
					},
				},
			},
			want: map[MutatorIngestionStatus]int{
				MutatorStatusActive: 1,
				MutatorStatusError:  0,
			},
		},
		{
			name: "two active mutators and one erroneous mutator",
			fields: fields{
				cache: map[types.ID]mutatorStatus{
					{Name: "foo"}: {
						ingestion: MutatorStatusActive,
					},
					{Name: "bar"}: {
						ingestion: MutatorStatusActive,
					},
					{Name: "baz"}: {
						ingestion: MutatorStatusError,
					},
				},
			},
			want: map[MutatorIngestionStatus]int{
				MutatorStatusActive: 2,
				MutatorStatusError:  1,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Cache{
				cache: tt.fields.cache,
				mux:   sync.RWMutex{},
			}
			if got := c.TallyStatus(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Cache.TallyStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}
