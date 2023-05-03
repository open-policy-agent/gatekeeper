package aggregator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// allowed keys.
	syncset    = "TODOa"
	configsync = "TODOb"
)

var (
	// test gvks.
	g1v1k1 = schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "Kind1"}
	g1v1k2 = schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "Kind2"}
)

type upsertKeyGVKs struct {
	key  KindName
	gvks []schema.GroupVersionKind
}

func Test_bidiGVKAggregator_UpsertWithValidation(t *testing.T) {
	// Define test cases with inputs and expected outputs
	tests := []struct {
		name string
		// each entry in the list is a new Upsert call
		keyGVKs     []upsertKeyGVKs
		expectAdded bool
		expectData  map[KindName]map[schema.GroupVersionKind]struct{}
		expectRev   map[schema.GroupVersionKind]map[KindName]struct{}
		expectErr   bool
	}{
		// error cases
		{
			name: "add new key with invalid kind",
			keyGVKs: []upsertKeyGVKs{
				{
					key: KindName{
						Kind: "c",
						Name: "bar",
					},
					gvks: []schema.GroupVersionKind{g1v1k1},
				},
			},
			expectErr: true,
		},
		// happy path cases
		{
			name: "add one key and GVKs",
			keyGVKs: []upsertKeyGVKs{
				{
					key: KindName{
						Kind: syncset,
						Name: "foo",
					},
					gvks: []schema.GroupVersionKind{g1v1k1, g1v1k2},
				},
			},
			expectAdded: true,
			expectData: map[KindName]map[schema.GroupVersionKind]struct{}{
				{
					Kind: syncset,
					Name: "foo",
				}: {
					g1v1k1: {},
					g1v1k2: {},
				},
			},
			expectRev: map[schema.GroupVersionKind]map[KindName]struct{}{
				g1v1k1: {
					{
						Kind: syncset,
						Name: "foo",
					}: {},
				},
				g1v1k2: {
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
					key: KindName{
						Kind: syncset,
						Name: "foo",
					},
					gvks: []schema.GroupVersionKind{g1v1k1, g1v1k2},
				},
				{
					key: KindName{
						Kind: configsync,
						Name: "foo",
					},
					gvks: []schema.GroupVersionKind{g1v1k1, g1v1k2},
				},
			},
			expectAdded: true,
			expectData: map[KindName]map[schema.GroupVersionKind]struct{}{
				{
					Kind: syncset,
					Name: "foo",
				}: {
					g1v1k1: {},
					g1v1k2: {},
				},
				{
					Kind: configsync,
					Name: "foo",
				}: {
					g1v1k1: {},
					g1v1k2: {},
				},
			},
			expectRev: map[schema.GroupVersionKind]map[KindName]struct{}{
				g1v1k1: {
					{
						Kind: syncset,
						Name: "foo",
					}: {},
					{
						Kind: configsync,
						Name: "foo",
					}: {},
				},
				g1v1k2: {
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
			tt := tt
			agg := NewGVKAggregator([]string{syncset, configsync})

			for _, keyGVKs := range tt.keyGVKs {
				err := agg.UpsertWithValidation(keyGVKs.key, keyGVKs.gvks)

				if tt.expectErr {
					assert.Error(t, err, "expected an error but none occurred")
					return
				}
			}

			require.Equal(t, tt.expectData, agg.store, "data map did not match expected")            //nolint:forcetypeassert
			require.Equal(t, tt.expectRev, agg.reverseStore, "reverse store did not match expected") //nolint:forcetypeassert
		})
	}
}
