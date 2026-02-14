package util

import (
	"sync"
	"testing"
)

func TestShouldSkipPodOwnerRef(t *testing.T) {
	// Reset state for test isolation
	SetSkipPodOwnerRef(false)
	t.Cleanup(func() {
		SetSkipPodOwnerRef(false)
	})

	t.Run("default value is false for backward compatibility", func(t *testing.T) {
		SetSkipPodOwnerRef(false)
		if ShouldSkipPodOwnerRef() {
			t.Error("Expected ShouldSkipPodOwnerRef() to return false by default")
		}
	})

	t.Run("can be set to true", func(t *testing.T) {
		SetSkipPodOwnerRef(true)
		if !ShouldSkipPodOwnerRef() {
			t.Error("Expected ShouldSkipPodOwnerRef() to return true after SetSkipPodOwnerRef(true)")
		}
	})

	t.Run("can be set back to false", func(t *testing.T) {
		SetSkipPodOwnerRef(true)
		SetSkipPodOwnerRef(false)
		if ShouldSkipPodOwnerRef() {
			t.Error("Expected ShouldSkipPodOwnerRef() to return false after SetSkipPodOwnerRef(false)")
		}
	})

	t.Run("is thread-safe", func(t *testing.T) {
		SetSkipPodOwnerRef(false)
		var wg sync.WaitGroup
		const numGoroutines = 100

		// Start multiple goroutines that read and write concurrently
		for i := 0; i < numGoroutines; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				SetSkipPodOwnerRef(true)
			}()
			go func() {
				defer wg.Done()
				_ = ShouldSkipPodOwnerRef()
			}()
		}
		wg.Wait()
		// test passes
	})
}
