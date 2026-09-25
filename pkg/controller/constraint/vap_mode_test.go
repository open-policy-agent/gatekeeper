package constraint

import (
	"errors"
	"testing"
)

func TestSetVAPGenerationMode(t *testing.T) {
	original := GetVAPGenerationMode()
	t.Cleanup(func() {
		if err := SetVAPGenerationMode(original); err != nil {
			t.Errorf("restore VAP generation mode: %v", err)
		}
	})

	for _, mode := range []VAPGenerationMode{VAPGenerationModeTemplate, VAPGenerationModeConstraint} {
		if err := SetVAPGenerationMode(mode); err != nil {
			t.Fatalf("SetVAPGenerationMode(%q): %v", mode, err)
		}
		if got := GetVAPGenerationMode(); got != mode {
			t.Fatalf("GetVAPGenerationMode() = %q, want %q", got, mode)
		}
	}

	if err := SetVAPGenerationMode("unsupported"); !errors.Is(err, ErrInvalidVAPGenerationMode) {
		t.Fatalf("SetVAPGenerationMode() error = %v, want %v", err, ErrInvalidVAPGenerationMode)
	}
}
