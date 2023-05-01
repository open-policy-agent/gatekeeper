package aggregator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type upsertKeyGVKs struct {
	key  Key
	gvks []schema.GroupVersionKind
}

func Test_bidiGVKAggregator_UpsertWithValidation(t *testing.T) {
	// Define test cases with inputs and expected outputs
	tests := []struct {
		name string
		// each entry in the list is a new Upsert call
		keyGVKs     []upsertKeyGVKs
		expectAdded bool
		expectData  map[Key]map[schema.GroupVersionKind]struct{}
		expectRev   map[schema.GroupVersionKind]map[Key]struct{}
		expectErr   bool
	}{
		// error cases
		{
			name: "add new key with invalid kind",
			keyGVKs: []upsertKeyGVKs{
				{
					key: Key{
						Kind: "c",
						Name: "bar",
					},
					gvks: []schema.GroupVersionKind{
						{
							Group:   "group1",
							Version: "v1",
							Kind:    "Kind1",
						},
					},
				},
			},
			expectErr: true,
		},
		// happy path cases
		{
			name: "add one key and GVKs",
			keyGVKs: []upsertKeyGVKs{
				{
					key: Key{
						Kind: syncset,
						Name: "foo",
					},
					gvks: []schema.GroupVersionKind{
						{
							Group:   "group1",
							Version: "v1",
							Kind:    "Kind1",
						},
						{
							Group:   "group2",
							Version: "v1",
							Kind:    "Kind2",
						},
					},
				},
			},
			expectAdded: true,
			expectData: map[Key]map[schema.GroupVersionKind]struct{}{
				{
					Kind: syncset,
					Name: "foo",
				}: {
					{
						Group:   "group1",
						Version: "v1",
						Kind:    "Kind1",
					}: {},
					{
						Group:   "group2",
						Version: "v1",
						Kind:    "Kind2",
					}: {},
				},
			},
			expectRev: map[schema.GroupVersionKind]map[Key]struct{}{
				{
					Group:   "group1",
					Version: "v1",
					Kind:    "Kind1",
				}: {
					{
						Kind: syncset,
						Name: "foo",
					}: {},
				},
				{
					Group:   "group2",
					Version: "v1",
					Kind:    "Kind2",
				}: {
					{
						Kind: syncset,
						Name: "foo",
					}: {},
				},
			},
		},
		{
			name: "add two keys and GVKs",
			keyGVKs: []upsertKeyGVKs{
				{
					key: Key{
						Kind: syncset,
						Name: "foo",
					},
					gvks: []schema.GroupVersionKind{
						{
							Group:   "group1",
							Version: "v1",
							Kind:    "Kind1",
						},
						{
							Group:   "group2",
							Version: "v1",
							Kind:    "Kind2",
						},
					},
				},
				{
					key: Key{
						Kind: configsync,
						Name: "foo",
					},
					gvks: []schema.GroupVersionKind{
						{
							Group:   "group1",
							Version: "v1",
							Kind:    "Kind1",
						},
						{
							Group:   "group2",
							Version: "v1",
							Kind:    "Kind2",
						},
					},
				},
			},
			expectAdded: true,
			expectData: map[Key]map[schema.GroupVersionKind]struct{}{
				{
					Kind: syncset,
					Name: "foo",
				}: {
					{
						Group:   "group1",
						Version: "v1",
						Kind:    "Kind1",
					}: {},
					{
						Group:   "group2",
						Version: "v1",
						Kind:    "Kind2",
					}: {},
				},
				{
					Kind: configsync,
					Name: "foo",
				}: {
					{
						Group:   "group1",
						Version: "v1",
						Kind:    "Kind1",
					}: {},
					{
						Group:   "group2",
						Version: "v1",
						Kind:    "Kind2",
					}: {},
				},
			},
			expectRev: map[schema.GroupVersionKind]map[Key]struct{}{
				{
					Group:   "group1",
					Version: "v1",
					Kind:    "Kind1",
				}: {
					{
						Kind: syncset,
						Name: "foo",
					}: {},
					{
						Kind: configsync,
						Name: "foo",
					}: {},
				},
				{
					Group:   "group2",
					Version: "v1",
					Kind:    "Kind2",
				}: {
					{
						Kind: syncset,
						Name: "foo",
					}: {},
					{
						Kind: configsync,
						Name: "foo",
					}: {},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agg := NewGVKAggregator()

			for _, keyGVKs := range tt.keyGVKs {
				added, err := agg.UpsertWithValidation(keyGVKs.key, keyGVKs.gvks)

				if tt.expectErr {
					assert.Error(t, err, "expected an error but none occurred")
					return
				}

				require.Equal(t, tt.expectAdded, added, "returned value did not match expected")
			}

			// these are open box tests for the underlying implementation of the GVKAggregator
			require.Equal(t, tt.expectData, agg.(*bidiGVKAggregator).store, "data map did not match expected")            //nolint:forcetypeassert
			require.Equal(t, tt.expectRev, agg.(*bidiGVKAggregator).reverseStore, "reverse store did not match expected") //nolint:forcetypeassert
		})
	}
}
