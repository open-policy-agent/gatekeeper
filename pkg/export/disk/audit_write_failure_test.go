package disk

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
)

// auditWriteFaultChildEnv marks the re-executed test process that lowers
// RLIMIT_FSIZE. The limit applies to every thread in a process, so it is never
// changed in the parent test process.
const auditWriteFaultChildEnv = "GATEKEEPER_DISK_AUDIT_WRITE_FAULT_CHILD"

const (
	auditFaultConnection = "audit-connection"
	auditFaultTopic      = "audit-channel"
	auditFaultViolation  = "you must provide labels: owner"
	priorAuditID         = "2026-10-28T09:55:00Z"
	failedAuditID        = "2026-10-28T10:00:00Z"
	blockedAuditID       = "2026-10-28T10:02:00Z"
	recoveryAuditID      = "2026-10-28T10:05:00Z"
)

// TestAuditWriteFailureAbortsRun forces real regular-file write failures with
// EFBIG and verifies that the failed run is never published, later runs stay
// isolated, and unrelated output on the same Connection path is preserved.
func TestAuditWriteFailureAbortsRun(t *testing.T) {
	if os.Getenv(auditWriteFaultChildEnv) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAuditWriteFailureAbortsRun$", "-test.v") //nolint:gosec // os.Args[0] is the current test binary and the test name is constant.
		cmd.Env = append(os.Environ(), auditWriteFaultChildEnv+"=1")
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "--- PASS: TestAuditWriteFailureAbortsRun (") {
			t.Fatalf("write fault child process failed: %v\n%s", err, out)
		}
		return
	}
	// Exceeding RLIMIT_FSIZE raises SIGXFSZ; ignoring it in this child makes the
	// write return EFBIG like other storage write errors.
	signal.Ignore(syscall.SIGXFSZ)

	for _, tc := range []struct {
		name string
		// failAt indexes the failed run's start, violation, second violation, and completion records.
		failAt         int
		allowedBytes   int64
		removalBlocked bool
	}{
		{name: "zero-byte start", failAt: 0},
		{name: "partial start", failAt: 0, allowedBytes: 12},
		{name: "zero-byte violation", failAt: 2},
		{name: "partial violation", failAt: 2, allowedBytes: 12},
		{name: "zero-byte completion", failAt: 3},
		{name: "partial completion", failAt: 3, allowedBytes: 12},
		{name: "completion with blocked removal", failAt: 3, removalBlocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.removalBlocked && os.Geteuid() == 0 {
				t.Skip("directory permissions do not prevent removal by root")
			}
			root := t.TempDir()
			topicDir := filepath.Join(root, auditFaultTopic)
			writer := newAdmissionWriter()
			createAdmissionConnection(t, writer, auditFaultConnection, root, 2)
			publish := func(msg any) error {
				return writer.Publish(context.Background(), auditFaultConnection, msg, auditFaultTopic)
			}

			// A completed audit and admission output share the Connection path and
			// must survive both the failure and recovery unchanged.
			priorRecords := auditRunRecords(priorAuditID, "prior-pod")
			for i := range priorRecords {
				if err := publish(priorRecords[i]); err != nil {
					t.Fatalf("Publish(prior %s) error = %v", priorRecords[i].Message, err)
				}
			}
			for _, name := range []string{"admitted-1", "admitted-2", "admitted-3"} {
				if err := publish(admissionPayload(t, name)); err != nil {
					t.Fatalf("Publish(admission %s) error = %v", name, err)
				}
			}
			preserved := readDirFiles(t, topicDir)
			if len(preserved) != 3 {
				t.Fatalf("expected prior audit, ready admission, and open admission files, got %v", mapKeys(preserved))
			}

			failedRecords := auditRunRecords(failedAuditID, "failed-pod-1", "failed-pod-2")
			for i := range tc.failAt {
				if err := publish(failedRecords[i]); err != nil {
					t.Fatalf("Publish(failed run %s) error = %v", failedRecords[i].Message, err)
				}
			}
			if tc.removalBlocked {
				setDirMode(t, topicDir, 0o555)
			}
			failedFile := filepath.Join(topicDir, auditRunFileName(failedAuditID, "txt"))
			restoreLimit := limitFileSize(t, fileSize(t, failedFile)+tc.allowedBytes)
			err := publish(failedRecords[tc.failAt])
			restoreLimit()
			if !errors.Is(err, syscall.EFBIG) {
				t.Errorf("failed write error = %v, want EFBIG", err)
			}
			if tc.removalBlocked && !errors.Is(err, fs.ErrPermission) {
				t.Errorf("failed write error = %v, want the incomplete file removal failure reported", err)
			}
			// The remaining records of the failed run must not reach its file.
			for i := tc.failAt + 1; i < len(failedRecords); i++ {
				if err := publish(failedRecords[i]); err == nil {
					t.Errorf("Publish(failed run %s) succeeded after the run failed", failedRecords[i].Message)
				}
			}
			if _, err := os.Stat(filepath.Join(topicDir, auditRunFileName(failedAuditID, "log"))); !os.IsNotExist(err) {
				t.Errorf("failed run was published as a ready file, stat error = %v", err)
			}

			recoveryID := recoveryAuditID
			if tc.removalBlocked {
				if _, err := os.Stat(failedFile); err != nil {
					t.Errorf("expected the unremovable incomplete file to remain owned, stat error = %v", err)
				}
				// The next run retries removal and must not start while it fails.
				blockedRecords := auditRunRecords(blockedAuditID, "blocked-pod")
				for i := range blockedRecords {
					if err := publish(blockedRecords[i]); err == nil {
						t.Errorf("Publish(blocked run %s) succeeded while the failed run could not be removed", blockedRecords[i].Message)
					} else if i == 0 && !errors.Is(err, fs.ErrPermission) {
						t.Errorf("blocked start error = %v, want the retried removal failure", err)
					}
				}
				setDirMode(t, topicDir, 0o770)
			}

			recoveryRecords := auditRunRecords(recoveryID, "recovery-pod")
			for i := range recoveryRecords {
				if err := publish(recoveryRecords[i]); err != nil {
					t.Errorf("Publish(recovery run %s) error = %v", recoveryRecords[i].Message, err)
				}
			}

			wantFiles := []string{auditRunFileName(priorAuditID, "log"), auditRunFileName(recoveryID, "log")}
			if gotFiles := auditFileNames(t, topicDir); !slices.Equal(gotFiles, wantFiles) {
				t.Errorf("audit files = %v, want %v; contents:\n%s", gotFiles, wantFiles, describeAuditFiles(t, topicDir, gotFiles))
			}
			assertAuditRun(t, filepath.Join(topicDir, auditRunFileName(recoveryID, "log")), recoveryID, "recovery-pod")
			for name, want := range preserved {
				got, err := os.ReadFile(filepath.Join(topicDir, name))
				if err != nil || string(got) != want {
					t.Errorf("preserved file %s changed: err = %v, content %q, want %q", name, err, got, want)
				}
			}
			if !openConnectionExists(writer, auditFaultConnection) {
				t.Error("recovery must not remove the Connection")
			}
		})
	}
}

func TestPublishAbortsRunWithReleasedDescriptor(t *testing.T) {
	writer := newTestWriter(nil)
	tmpPath := t.TempDir()
	requireCreateConnection(t, writer, "conn1", diskConfig(tmpPath, 1))
	if err := writer.Publish(context.Background(), "conn1", exportutil.ExportMsg{ID: "audit1", Message: exportutil.AuditStartedMsg}, "topic1"); err != nil {
		t.Fatalf("Publish(audit start) error = %v", err)
	}
	// A failed close still releases an os.File descriptor; model that state.
	writer.mu.Lock()
	file := writer.openConnections["conn1"].File
	writer.mu.Unlock()
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	err := writer.Publish(context.Background(), "conn1", exportutil.ExportMsg{ID: "audit1", Message: exportutil.AuditCompletedMsg}, "topic1")
	if !errors.Is(err, os.ErrClosed) || strings.Contains(err.Error(), "closing aborted audit file") {
		t.Fatalf("expected only the closed-file write error, got %v", err)
	}
	for _, name := range []string{"audit1.txt", "audit1.log"} {
		if _, err := os.Stat(filepath.Join(tmpPath, "topic1", name)); !os.IsNotExist(err) {
			t.Fatalf("expected aborted run file %s to be absent, stat error = %v", name, err)
		}
	}
	for _, message := range []string{exportutil.AuditStartedMsg, exportutil.AuditCompletedMsg} {
		if err := writer.Publish(context.Background(), "conn1", exportutil.ExportMsg{ID: "audit2", Message: message}, "topic1"); err != nil {
			t.Fatalf("Publish(next audit %s) error = %v", message, err)
		}
	}
	if _, err := os.Stat(filepath.Join(tmpPath, "topic1", "audit2.log")); err != nil {
		t.Fatalf("expected next audit run to be published: %v", err)
	}
}

func auditRunRecords(auditID string, resources ...string) []exportutil.ExportMsg {
	records := []exportutil.ExportMsg{{ID: auditID, Message: exportutil.AuditStartedMsg}}
	for _, resource := range resources {
		records = append(records, exportutil.ExportMsg{
			ID:                auditID,
			EventType:         "violation_audited",
			Kind:              "K8sRequiredLabels",
			Name:              "must-have-owner",
			Message:           auditFaultViolation,
			EnforcementAction: "deny",
			ResourceKind:      "Pod",
			ResourceNamespace: "default",
			ResourceName:      resource,
		})
	}
	return append(records, exportutil.ExportMsg{ID: auditID, Message: exportutil.AuditCompletedMsg})
}

func auditRunFileName(auditID, extension string) string {
	return strings.ReplaceAll(auditID, ":", "_") + "." + extension
}

// limitFileSize lowers only the soft limit and returns a function that restores
// and verifies the original limit.
func limitFileSize(t *testing.T, limit int64) func() {
	t.Helper()
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Fatalf("Getrlimit() error = %v", err)
	}
	if limit < 0 {
		t.Fatalf("negative file size limit %d", limit)
	}
	soft := uint64(limit) // #nosec G115 -- negative limits are rejected above.
	if soft > original.Max {
		t.Fatalf("file size limit %d exceeds hard limit %d", soft, original.Max)
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: soft, Max: original.Max}); err != nil {
		t.Fatalf("Setrlimit() error = %v", err)
	}
	return func() {
		t.Helper()
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
			t.Fatalf("restoring RLIMIT_FSIZE: %v", err)
		}
		var restored syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &restored); err != nil || restored != original {
			t.Fatalf("RLIMIT_FSIZE after restore = %+v (error %v), want %+v", restored, err, original)
		}
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	return info.Size()
}

func setDirMode(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatalf("Chmod(%s, %o) error = %v", dir, mode, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o770) })
}

func readDirFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s) error = %v", dir, err)
	}
	files := make(map[string]string, len(entries))
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", entry.Name(), err)
		}
		files[entry.Name()] = string(content)
	}
	return files
}

func mapKeys(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for name := range files {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	return keys
}

// auditFileNames returns every non-admission entry, including incomplete .txt files.
func auditFileNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s) error = %v", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), admissionFilePrefix) {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	return names
}

func describeAuditFiles(t *testing.T, dir string, names []string) string {
	t.Helper()
	var description strings.Builder
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			description.WriteString(name + ": " + err.Error() + "\n")
			continue
		}
		description.WriteString(name + ":\n")
		for _, line := range strings.SplitAfter(string(content), "\n") {
			if line == "" {
				continue
			}
			var record exportutil.ExportMsg
			if err := json.Unmarshal([]byte(line), &record); err != nil || !strings.HasSuffix(line, "\n") {
				description.WriteString("  malformed " + strings.TrimSuffix(line, "\n") + "\n")
				continue
			}
			description.WriteString("  " + record.ID + " " + record.Message + " " + record.ResourceName + "\n")
		}
	}
	return description.String()
}

// assertAuditRun decodes every JSONL line and requires exactly one run's start,
// violation, and completion records in order.
func assertAuditRun(t *testing.T, path, auditID, resource string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("ReadFile(%s) error = %v", path, err)
		return
	}
	if !strings.HasSuffix(string(content), "\n") {
		t.Errorf("%s does not end with a complete JSONL record: %q", path, content)
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	want := auditRunRecords(auditID, resource)
	if len(lines) != len(want) {
		t.Errorf("%s has %d records, want %d: %q", path, len(lines), len(want), content)
		return
	}
	for i, line := range lines {
		var got exportutil.ExportMsg
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Errorf("%s line %d is not valid JSON: %q", path, i+1, line)
			continue
		}
		if got.ID != want[i].ID || got.Message != want[i].Message || got.EventType != want[i].EventType || got.ResourceName != want[i].ResourceName {
			t.Errorf("%s line %d = %+v, want %+v", path, i+1, got, want[i])
		}
	}
}
