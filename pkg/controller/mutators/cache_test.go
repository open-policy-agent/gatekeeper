package mutators

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
)

func TestSharedCache_TallyStatusAcrossKinds(t *testing.T) {
	cache := NewMutationCache()

	for i := 0; i < 4; i++ {
		cache.Upsert(types.ID{
			Group: "mutations.gatekeeper.sh",
			Kind:  "Assign",
			Name:  fmt.Sprintf("assign-%d", i),
		}, MutatorStatusActive, false)
	}

	for i := 0; i < 2; i++ {
		cache.Upsert(types.ID{
			Group: "mutations.gatekeeper.sh",
			Kind:  "ModifySet",
			Name:  fmt.Sprintf("modifyset-%d", i),
		}, MutatorStatusActive, false)
	}

	cache.Upsert(types.ID{
		Group: "mutations.gatekeeper.sh",
		Kind:  "AssignMetadata",
		Name:  "assignmeta-0",
	}, MutatorStatusActive, false)

	got := cache.TallyStatus()

	// All 7 mutators should be tallied together in the shared cache.
	if got[MutatorStatusActive] != 7 {
		t.Errorf("shared Cache.TallyStatus()[active] = %d, want 7", got[MutatorStatusActive])
	}
	if got[MutatorStatusError] != 0 {
		t.Errorf("shared Cache.TallyStatus()[error] = %d, want 0", got[MutatorStatusError])
	}
}

func TestSeparateCaches_TallyStatusMissesOtherKinds(t *testing.T) {
	assignCache := NewMutationCache()
	modifySetCache := NewMutationCache()
	assignMetaCache := NewMutationCache()

	for i := 0; i < 4; i++ {
		assignCache.Upsert(types.ID{
			Group: "mutations.gatekeeper.sh",
			Kind:  "Assign",
			Name:  fmt.Sprintf("assign-%d", i),
		}, MutatorStatusActive, false)
	}
	for i := 0; i < 2; i++ {
		modifySetCache.Upsert(types.ID{
			Group: "mutations.gatekeeper.sh",
			Kind:  "ModifySet",
			Name:  fmt.Sprintf("modifyset-%d", i),
		}, MutatorStatusActive, false)
	}
	assignMetaCache.Upsert(types.ID{
		Group: "mutations.gatekeeper.sh",
		Kind:  "AssignMetadata",
		Name:  "assignmeta-0",
	}, MutatorStatusActive, false)

	if got := assignCache.TallyStatus()[MutatorStatusActive]; got != 4 {
		t.Errorf("assignCache.TallyStatus()[active] = %d, want 4", got)
	}
	if got := modifySetCache.TallyStatus()[MutatorStatusActive]; got != 2 {
		t.Errorf("modifySetCache.TallyStatus()[active] = %d, want 2", got)
	}
	if got := assignMetaCache.TallyStatus()[MutatorStatusActive]; got != 1 {
		t.Errorf("assignMetaCache.TallyStatus()[active] = %d, want 1", got)
	}
}

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
