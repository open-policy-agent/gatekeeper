package aggregator

import (
	"fmt"
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
	emptyGVK = schema.GroupVersionKind{Group: "", Version: "", Kind: ""}

	g1v1k1 = schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "Kind1"}
	g1v1k2 = schema.GroupVersionKind{Group: "group1", Version: "v1", Kind: "Kind2"}

	g2v1k1 = schema.GroupVersionKind{Group: "group2", Version: "v1", Kind: "Kind1"}
	g2v1k2 = schema.GroupVersionKind{Group: "group2", Version: "v1", Kind: "Kind2"}

	g1v2k1 = schema.GroupVersionKind{Group: "group1", Version: "v2", Kind: "Kind1"}
	g1v2k2 = schema.GroupVersionKind{Group: "group1", Version: "v2", Kind: "Kind2"}

	g2v2k1 = schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "Kind1"}
	g2v2k2 = schema.GroupVersionKind{Group: "group2", Version: "v2", Kind: "Kind2"}

	g3v1k1 = schema.GroupVersionKind{Group: "group3", Version: "v1", Kind: "Kind1"}
	g3v1k2 = schema.GroupVersionKind{Group: "group3", Version: "v1", Kind: "Kind2"}
)

type keyedGVKs struct {
	key  Key
	gvks []schema.GroupVersionKind
}

func Test_GVKAggregator_Upsert(t *testing.T) {
	tests := []struct {
		name string
		// each entry in the list is a new Upsert call
		keyGVKs []keyedGVKs

		expectData map[Key]map[schema.GroupVersionKind]struct{}
		expectRev  map[schema.GroupVersionKind]map[Key]struct{}
	}{
		{
			name: "empty GVKs",
			keyGVKs: []keyedGVKs{
				{
					key: Key{
						Source: syncset,
						ID:     "foo",
					},
					gvks: []schema.GroupVersionKind{emptyGVK, emptyGVK},
				},
			},
			expectData: map[Key]map[schema.GroupVersionKind]struct{}{},
			expectRev:  map[schema.GroupVersionKind]map[Key]struct{}{},
		},
		{
			name: "add one key and GVKs",
			keyGVKs: []keyedGVKs{
				{
					key: Key{
						Source: syncset,
						ID:     "foo",
					},
					gvks: []schema.GroupVersionKind{g1v1k1, g1v1k2, emptyGVK},
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
			keyGVKs: []keyedGVKs{
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
		{
			name: "add one key and overwrite it",
			keyGVKs: []keyedGVKs{
				{
					key: Key{
						Source: syncset,
						ID:     "foo",
					},
					gvks: []schema.GroupVersionKind{g1v1k1, g1v1k2},
				},
				{
					key: Key{
						Source: syncset,
						ID:     "foo",
					},
					gvks: []schema.GroupVersionKind{g3v1k1, g3v1k2},
				},
			},
			expectData: map[Key]map[schema.GroupVersionKind]struct{}{
				{
					Source: syncset,
					ID:     "foo",
				}: {
					g3v1k1: {},
					g3v1k2: {},
				},
			},
			expectRev: map[schema.GroupVersionKind]map[Key]struct{}{
				g3v1k1: {
					{
						Source: syncset,
						ID:     "foo",
					}: {},
				},
				g3v1k2: {
					{
						Source: syncset,
						ID:     "foo",
					}: {},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt := tt
			agg := NewGVKAggregator()

			for _, keyGVKs := range tt.keyGVKs {
				agg.Upsert(keyGVKs.key, keyGVKs.gvks)
			}

			// require all gvks added to be present in the aggregator
			require.Equal(t, tt.expectData, agg.store, "data map did not match expected")
			require.Equal(t, tt.expectRev, agg.reverseStore, "reverse store did not match expected")
		})
	}
}

func Test_GVKAgreggator_Remove(t *testing.T) {
	t.Run("Remove on empty aggregator", func(t *testing.T) {
		b := NewGVKAggregator()
		key := Key{Source: "testSource", ID: "testID"}
		b.Remove(key)
	})

	t.Run("Remove non-existing key", func(t *testing.T) {
		b := NewGVKAggregator()
		key1 := Key{Source: syncset, ID: "testID1"}
		key2 := Key{Source: configsync, ID: "testID2"}
		gvks := []schema.GroupVersionKind{g1v1k1, g1v1k2}
		b.Upsert(key1, gvks)
		b.Remove(key2)
	})

	t.Run("Remove existing key and verify reverseStore", func(t *testing.T) {
		b := NewGVKAggregator()
		key1 := Key{Source: syncset, ID: "testID"}
		gvks := []schema.GroupVersionKind{g1v1k1, g1v1k2}
		b.Upsert(key1, gvks)
		b.Remove(key1)

		for _, gvk := range gvks {
			require.False(t, b.IsPresent(gvk))
		}
	})

	t.Run("Remove 1 of existing keys referencing GVKs", func(t *testing.T) {
		b := NewGVKAggregator()
		key1 := Key{Source: syncset, ID: "testID"}
		key2 := Key{Source: configsync, ID: "testID"}
		gvks := []schema.GroupVersionKind{g1v1k1, g1v1k2}
		b.Upsert(key1, gvks)
		b.Upsert(key2, gvks)
		b.Remove(key1)

		for _, gvk := range gvks {
			require.True(t, b.IsPresent(gvk))
		}
	})
}

// Test_GVKAggreggator_E2E is a test that:
// - Upserts two sources with different GVKs
// - Upserts the two sources with some overlapping GVKs
// - Remove one key, tests for correct output
// - Overwrite an existing key with new data, and confirms correct re-calculation.
func Test_GVKAggreggator_E2E(t *testing.T) {
	b := NewGVKAggregator()

	key1 := Key{Source: syncset, ID: "testID"}
	key2 := Key{Source: configsync, ID: "testID"}

	gvksKind1 := []schema.GroupVersionKind{g1v1k1, g2v1k1, g2v2k1}
	gvksKind2 := []schema.GroupVersionKind{g1v1k2, g2v1k2, g2v2k2}

	b.Upsert(key1, gvksKind1) // key1 now has: g1v1k1, g2v1k1, g2v2k1
	b.Upsert(key2, gvksKind2) // key2 now has: g1v1k2, g2v1k2, g2v2k2

	// require that every gvk that was just added to be present
	gvksThatShouldBeTracked := map[schema.GroupVersionKind]interface{}{
		g1v1k1: struct{}{}, g2v1k1: struct{}{}, g2v2k1: struct{}{},
		g1v1k2: struct{}{}, g2v1k2: struct{}{}, g2v2k2: struct{}{},
	}
	for gvk := range gvksThatShouldBeTracked {
		require.True(t, b.IsPresent(gvk))
	}

	// notice the overlap of with gvksKind1, gvksKind2
	gvksVersion1 := []schema.GroupVersionKind{g1v1k1, g1v1k2, g2v1k1, g2v1k2} // overlaps key2 with g1v1k2, g1v1k2
	gvksVersion2 := []schema.GroupVersionKind{g1v2k1, g1v2k2, g2v2k1, g2v2k2} // overlaps key1 with g2v2k1; overlaps key2 with g2v2k2

	// new upserts
	b.Upsert(key1, gvksVersion1) // key1 no longer associates g2v2k1, but key2 does
	b.Upsert(key2, gvksVersion2) // key2 no longer associaates g1v1k2, but key1 does

	// require that every gvk that was just added now and before to be present
	for _, gvk := range append(append([]schema.GroupVersionKind{}, gvksVersion1...), gvksVersion2...) {
		gvksThatShouldBeTracked[gvk] = struct{}{}

		require.True(t, b.IsPresent(gvk), fmt.Sprintf("gvk %s should be present", gvk))
	}

	// At this point
	// key1 has: g1v1k1, g2v1k1, g1v1k2, g2v1k2
	// key2 has: g2v2k2, g1v2k1, g1v2k2, g2v2k1
	// now remove key1
	b.Remove(key1)

	// untrack gvks that shouldn't exist in the
	for _, gvk := range []schema.GroupVersionKind{g1v1k1, g2v1k1, g1v1k2, g2v1k2} {
		delete(gvksThatShouldBeTracked, gvk)

		// also require that this gvk not be present since it was removed
		require.False(t, b.IsPresent(gvk), fmt.Sprintf("gvk %s shouldn't be present", gvk))
	}

	// require GVKs from un-removed keys that have GVK associations to still be present
	for gvk := range gvksThatShouldBeTracked {
		require.True(t, b.IsPresent(gvk), fmt.Sprintf("gvk %s should be present", gvk))
	}

	// overwrite key2
	gvksGroup3 := []schema.GroupVersionKind{g3v1k1, g3v1k2}
	b.Upsert(key2, gvksGroup3)

	// require all previously added gvks to not be present:
	testPresenceForGVK(t, false, b, gvksKind1...)
	testPresenceForGVK(t, false, b, gvksKind2...)
	testPresenceForGVK(t, false, b, gvksVersion1...)
	testPresenceForGVK(t, false, b, gvksVersion2...)

	// require newly added gvks to be present
	testPresenceForGVK(t, true, b, gvksGroup3...)
}

func testPresenceForGVK(t *testing.T, requireTrue bool, b *GVKAgreggator, gvks ...schema.GroupVersionKind) {
	t.Helper()

	var msg string
	if requireTrue {
		msg = "%s should be present"
	} else {
		msg = "%s shouldn't be present"
	}
	for _, gvk := range gvks {
		require.Equal(t, requireTrue, b.IsPresent(gvk), fmt.Sprintf(msg, gvk))
	}
}
