package metrics

import (
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func TestNewVAPStatusRegistry(t *testing.T) {
	r := NewVAPStatusRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.cache == nil {
		t.Fatal("expected non-nil cache")
	}
	if len(r.cache) != 0 {
		t.Fatal("expected empty cache")
	}
}

func TestVAPStatusRegistry_Add(t *testing.T) {
	tests := []struct {
		name           string
		operations     []struct {
			key    types.NamespacedName
			status VAPStatus
		}
		expectedCache map[types.NamespacedName]VAPStatus
	}{
		{
			name: "add single entry",
			operations: []struct {
				key    types.NamespacedName
				status VAPStatus
			}{
				{key: types.NamespacedName{Name: "test1"}, status: VAPStatusActive},
			},
			expectedCache: map[types.NamespacedName]VAPStatus{
				{Name: "test1"}: VAPStatusActive,
			},
		},
		{
			name: "add multiple entries with different statuses",
			operations: []struct {
				key    types.NamespacedName
				status VAPStatus
			}{
				{key: types.NamespacedName{Name: "test1"}, status: VAPStatusActive},
				{key: types.NamespacedName{Name: "test2"}, status: VAPStatusError},
				{key: types.NamespacedName{Name: "test3", Namespace: "ns1"}, status: VAPStatusActive},
			},
			expectedCache: map[types.NamespacedName]VAPStatus{
				{Name: "test1"}:                 VAPStatusActive,
				{Name: "test2"}:                 VAPStatusError,
				{Name: "test3", Namespace: "ns1"}: VAPStatusActive,
			},
		},
		{
			name: "update existing entry with different status",
			operations: []struct {
				key    types.NamespacedName
				status VAPStatus
			}{
				{key: types.NamespacedName{Name: "test1"}, status: VAPStatusActive},
				{key: types.NamespacedName{Name: "test1"}, status: VAPStatusError},
			},
			expectedCache: map[types.NamespacedName]VAPStatus{
				{Name: "test1"}: VAPStatusError,
			},
		},
		{
			name: "add same entry with same status is no-op",
			operations: []struct {
				key    types.NamespacedName
				status VAPStatus
			}{
				{key: types.NamespacedName{Name: "test1"}, status: VAPStatusActive},
				{key: types.NamespacedName{Name: "test1"}, status: VAPStatusActive},
			},
			expectedCache: map[types.NamespacedName]VAPStatus{
				{Name: "test1"}: VAPStatusActive,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewVAPStatusRegistry()
			for _, op := range tt.operations {
				r.Add(op.key, op.status)
			}
			if len(r.cache) != len(tt.expectedCache) {
				t.Errorf("cache length = %d, want %d", len(r.cache), len(tt.expectedCache))
			}
			for key, expectedStatus := range tt.expectedCache {
				if status, ok := r.cache[key]; !ok {
					t.Errorf("key %v not found in cache", key)
				} else if status != expectedStatus {
					t.Errorf("cache[%v] = %v, want %v", key, status, expectedStatus)
				}
			}
		})
	}
}

func TestVAPStatusRegistry_Remove(t *testing.T) {
	tests := []struct {
		name          string
		initialCache  map[types.NamespacedName]VAPStatus
		removeKey     types.NamespacedName
		expectedCache map[types.NamespacedName]VAPStatus
	}{
		{
			name: "remove existing entry",
			initialCache: map[types.NamespacedName]VAPStatus{
				{Name: "test1"}: VAPStatusActive,
				{Name: "test2"}: VAPStatusError,
			},
			removeKey: types.NamespacedName{Name: "test1"},
			expectedCache: map[types.NamespacedName]VAPStatus{
				{Name: "test2"}: VAPStatusError,
			},
		},
		{
			name: "remove non-existing entry is no-op",
			initialCache: map[types.NamespacedName]VAPStatus{
				{Name: "test1"}: VAPStatusActive,
			},
			removeKey: types.NamespacedName{Name: "nonexistent"},
			expectedCache: map[types.NamespacedName]VAPStatus{
				{Name: "test1"}: VAPStatusActive,
			},
		},
		{
			name:          "remove from empty registry",
			initialCache:  map[types.NamespacedName]VAPStatus{},
			removeKey:     types.NamespacedName{Name: "test1"},
			expectedCache: map[types.NamespacedName]VAPStatus{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewVAPStatusRegistry()
			for k, v := range tt.initialCache {
				r.cache[k] = v
			}
			r.Remove(tt.removeKey)
			if len(r.cache) != len(tt.expectedCache) {
				t.Errorf("cache length = %d, want %d", len(r.cache), len(tt.expectedCache))
			}
			for key, expectedStatus := range tt.expectedCache {
				if status, ok := r.cache[key]; !ok {
					t.Errorf("key %v not found in cache", key)
				} else if status != expectedStatus {
					t.Errorf("cache[%v] = %v, want %v", key, status, expectedStatus)
				}
			}
		})
	}
}

func TestVAPStatusRegistry_ComputeTotals(t *testing.T) {
	tests := []struct {
		name           string
		cache          map[types.NamespacedName]VAPStatus
		expectedTotals map[VAPStatus]int64
	}{
		{
			name:           "empty registry",
			cache:          map[types.NamespacedName]VAPStatus{},
			expectedTotals: map[VAPStatus]int64{},
		},
		{
			name: "single status type",
			cache: map[types.NamespacedName]VAPStatus{
				{Name: "test1"}: VAPStatusActive,
				{Name: "test2"}: VAPStatusActive,
				{Name: "test3"}: VAPStatusActive,
			},
			expectedTotals: map[VAPStatus]int64{
				VAPStatusActive: 3,
			},
		},
		{
			name: "multiple status types",
			cache: map[types.NamespacedName]VAPStatus{
				{Name: "test1"}: VAPStatusActive,
				{Name: "test2"}: VAPStatusActive,
				{Name: "test3"}: VAPStatusError,
				{Name: "test4"}: VAPStatusError,
				{Name: "test5"}: VAPStatusError,
			},
			expectedTotals: map[VAPStatus]int64{
				VAPStatusActive: 2,
				VAPStatusError:  3,
			},
		},
		{
			name: "with namespaced keys",
			cache: map[types.NamespacedName]VAPStatus{
				{Name: "test1", Namespace: "ns1"}: VAPStatusActive,
				{Name: "test1", Namespace: "ns2"}: VAPStatusError,
				{Name: "test2", Namespace: "ns1"}: VAPStatusActive,
			},
			expectedTotals: map[VAPStatus]int64{
				VAPStatusActive: 2,
				VAPStatusError:  1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewVAPStatusRegistry()
			for k, v := range tt.cache {
				r.cache[k] = v
			}
			totals := r.ComputeTotals()
			if len(totals) != len(tt.expectedTotals) {
				t.Errorf("totals length = %d, want %d", len(totals), len(tt.expectedTotals))
			}
			for status, expectedCount := range tt.expectedTotals {
				if count := totals[status]; count != expectedCount {
					t.Errorf("totals[%v] = %d, want %d", status, count, expectedCount)
				}
			}
		})
	}
}

func TestVAPStatusRegistry_Concurrency(t *testing.T) {
	r := NewVAPStatusRegistry()
	const numGoroutines = 100
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3)

	// Concurrent adds
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := types.NamespacedName{Name: "test", Namespace: string(rune('a' + (id+j)%26))}
				status := VAPStatusActive
				if (id+j)%2 == 0 {
					status = VAPStatusError
				}
				r.Add(key, status)
			}
		}(i)
	}

	// Concurrent removes
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := types.NamespacedName{Name: "test", Namespace: string(rune('a' + (id+j)%26))}
				r.Remove(key)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = r.ComputeTotals()
			}
		}()
	}

	wg.Wait()

	r.mu.RLock()
	cacheLen := len(r.cache)
	r.mu.RUnlock()

	totals := r.ComputeTotals()
	var totalCount int64
	for _, count := range totals {
		totalCount += count
	}
	if totalCount != int64(cacheLen) {
		t.Errorf("totals sum (%d) != cache length (%d)", totalCount, cacheLen)
	}

	testKey := types.NamespacedName{Name: "post-concurrent-test", Namespace: "verify"}
	r.Add(testKey, VAPStatusActive)

	r.mu.RLock()
	status, exists := r.cache[testKey]
	r.mu.RUnlock()

	if !exists {
		t.Error("registry failed to add entry after concurrent operations")
	}
	if status != VAPStatusActive {
		t.Errorf("expected VAPStatusActive, got %v", status)
	}

	r.Remove(testKey)
	r.mu.RLock()
	_, exists = r.cache[testKey]
	r.mu.RUnlock()

	if exists {
		t.Error("registry failed to remove entry after concurrent operations")
	}
}

func TestVAPStatusRegistry_AddUpdateRemoveCycle(t *testing.T) {
	r := NewVAPStatusRegistry()
	key := types.NamespacedName{Name: "test", Namespace: "default"}

	r.Add(key, VAPStatusActive)
	totals := r.ComputeTotals()
	if totals[VAPStatusActive] != 1 {
		t.Errorf("expected 1 active, got %d", totals[VAPStatusActive])
	}
	if totals[VAPStatusError] != 0 {
		t.Errorf("expected 0 error, got %d", totals[VAPStatusError])
	}

	r.Add(key, VAPStatusError)
	totals = r.ComputeTotals()
	if totals[VAPStatusActive] != 0 {
		t.Errorf("expected 0 active after update, got %d", totals[VAPStatusActive])
	}
	if totals[VAPStatusError] != 1 {
		t.Errorf("expected 1 error after update, got %d", totals[VAPStatusError])
	}

	r.Remove(key)
	totals = r.ComputeTotals()
	if totals[VAPStatusActive] != 0 {
		t.Errorf("expected 0 active after remove, got %d", totals[VAPStatusActive])
	}
	if totals[VAPStatusError] != 0 {
		t.Errorf("expected 0 error after remove, got %d", totals[VAPStatusError])
	}
}
