package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindOldestReadyFileAndDelete(t *testing.T) {
	root := t.TempDir()
	topic := filepath.Join(root, "admission")
	if err := os.Mkdir(topic, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	oldest := filepath.Join(topic, admissionFilePrefix+"old.log")
	newest := filepath.Join(topic, admissionFilePrefix+"new.log")
	audit := filepath.Join(topic, "audit-run.log")
	if err := os.WriteFile(oldest, []byte("old\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(oldest) error = %v", err)
	}
	if err := os.WriteFile(newest, []byte("new\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(newest) error = %v", err)
	}
	if err := os.WriteFile(audit, []byte("audit\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(audit) error = %v", err)
	}
	oldTime := time.Now().Add(-time.Minute)
	if err := os.Chtimes(oldest, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	got, err := findOldestReadyFile(root)
	if err != nil {
		t.Fatalf("findOldestReadyFile() error = %v", err)
	}
	if got != oldest {
		t.Fatalf("expected %s, got %s", oldest, got)
	}
	newestGot, err := findNewestReadyFile(root)
	if err != nil {
		t.Fatalf("findNewestReadyFile() error = %v", err)
	}
	if newestGot != audit {
		t.Fatalf("expected audit file %s, got %s", audit, newestGot)
	}
	if err := processReadyFile(got, true); err != nil {
		t.Fatalf("processReadyFile() error = %v", err)
	}
	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Fatalf("expected processed file removal, got %v", err)
	}
}

func TestFindNextAuditReadyFile(t *testing.T) {
	root := t.TempDir()
	topic := filepath.Join(root, "audit")
	if err := os.Mkdir(topic, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	first := filepath.Join(topic, "first.log")
	if err := os.WriteFile(first, []byte("first\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(first) error = %v", err)
	}

	got, err := findNextReadyFile(root, false, "")
	if err != nil {
		t.Fatalf("findNextReadyFile(first) error = %v", err)
	}
	if got != first {
		t.Fatalf("expected %s, got %s", first, got)
	}
	if _, err := findNextReadyFile(root, false, first); !errors.Is(err, errNoNewReadyFile) {
		t.Fatalf("expected no new audit file, got %v", err)
	}

	second := filepath.Join(topic, "second.log")
	if err := os.WriteFile(second, []byte("second\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(second) error = %v", err)
	}
	newTime := time.Now().Add(time.Second)
	if err := os.Chtimes(second, newTime, newTime); err != nil {
		t.Fatalf("Chtimes(second) error = %v", err)
	}
	got, err = findNextReadyFile(root, false, first)
	if err != nil {
		t.Fatalf("findNextReadyFile(second) error = %v", err)
	}
	if got != second {
		t.Fatalf("expected %s, got %s", second, got)
	}
}
