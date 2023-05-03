package aggregator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// common test keys.
	syncset    = "TODOa"
	configsync = "TODOb"
)

var (
	// test gvks.
	g1v1k1 = schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "Kind1"}
	g1v1k2 = schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "Kind2"}
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
		keyGVKs []upsertKeyGVKs

		expectData map[Key]map[schema.GroupVersionKind]struct{}
		expectRev  map[schema.GroupVersionKind]map[Key]struct{}
	}{
		{
			name: "add one key and GVKs",
			keyGVKs: []upsertKeyGVKs{
				{
					key: Key{
						Source: syncset,
						ID:     "foo",
					},
					gvks: []schema.GroupVersionKind{g1v1k1, g1v1k2},
				},
			},
			expectData: map[Key]map[schema.GroupVersionKind]struct{}{
				{
					Source: syncset,
					ID:     "foo",
				}: {
					g1v1k1: {},
					g1v1k2: {},
				},
			},
			expectRev: map[schema.GroupVersionKind]map[Key]struct{}{
				g1v1k1: {
					{
						Source: syncset,
						ID:     "foo",
					}: {},
				},
				g1v1k2: {
					{
						Source: syncset,
						ID:     "foo",
					}: {},
				},
			},
		},
		{
			name: "add two keys and GVKs",
			keyGVKs: []upsertKeyGVKs{
				{
					key: Key{
						Source: syncset,
						ID:     "foo",
					},
					gvks: []schema.GroupVersionKind{g1v1k1, g1v1k2},
				},
				{
					key: Key{
						Source: configsync,
						ID:     "foo",
					},
					gvks: []schema.GroupVersionKind{g1v1k1, g1v1k2},
				},
			},
			expectData: map[Key]map[schema.GroupVersionKind]struct{}{
				{
					Source: syncset,
					ID:     "foo",
				}: {
					g1v1k1: {},
					g1v1k2: {},
				},
				{
					Source: configsync,
					ID:     "foo",
				}: {
					g1v1k1: {},
					g1v1k2: {},
				},
			},
			expectRev: map[schema.GroupVersionKind]map[Key]struct{}{
				g1v1k1: {
					{
						Source: syncset,
						ID:     "foo",
					}: {},
					{
						Source: configsync,
						ID:     "foo",
					}: {},
				},
				g1v1k2: {
					{
						Source: syncset,
						ID:     "foo",
					}: {},
					{
						Source: configsync,
						ID:     "foo",
					}: {},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt := tt
			agg := NewGVKAggregator([]string{syncset, configsync})

			for _, keyGVKs := range tt.keyGVKs {
				agg.Upsert(keyGVKs.key, keyGVKs.gvks)
			}

			require.Equal(t, tt.expectData, agg.store, "data map did not match expected")            //nolint:forcetypeassert
			require.Equal(t, tt.expectRev, agg.reverseStore, "reverse store did not match expected") //nolint:forcetypeassert
		})
	}
}
