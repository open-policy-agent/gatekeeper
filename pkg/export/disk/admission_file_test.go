package disk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
)

const completeAdmissionRecord = "{\"complete\":true}\n"

func admissionDiskConfig(path string) map[string]interface{} {
	return map[string]interface{}{
		"path":            path,
		"maxAuditResults": float64(2),
	}
}

func newAdmissionWriter() *Writer {
	return &Writer{
		openConnections:              make(map[string]Connection),
		closedConnections:            make(map[string]FailedConnection),
		cleanupDone:                  make(chan struct{}),
		closeAndRemoveFilesWithRetry: closeAndRemoveFilesWithRetry,
	}
}

func createAdmissionConnection(t *testing.T, writer *Writer, connectionName, path string, maxRecords int) {
	t.Helper()
	if err := writer.CreateConnection(context.Background(), connectionName, admissionDiskConfig(path)); err != nil {
		t.Fatalf("CreateConnection() error = %v", err)
	}
	// The cleanup janitor reschedules itself, so close the connection when the
	// test ends rather than leaving timers running against a deleted TempDir.
	t.Cleanup(func() { _ = writer.CloseConnection(connectionName) })
	updateAdmissionTestConnection(t, writer, connectionName, func(conn *Connection) {
		conn.admission.limits.maxResults = 2
		conn.admission.limits.maxFileBytes = 4096
		conn.admission.limits.maxFileRecords = int64(maxRecords)
		conn.admission.limits.maxFileAge = time.Hour
		conn.admission.limits.maxTotalBytes = 8192
		conn.admission.limits.fileTTL = 24 * time.Hour
		conn.admission.limits.maxRecordBytes = 2048
		conn.admission.limits.minFreeBytes = 0
	})
}

func updateAdmissionTestConnection(t *testing.T, writer *Writer, connectionName string, update func(*Connection)) {
	t.Helper()
	// Follow the production lock order so a janitor that already copied the
	// Connection cannot write its stale copy back over these limits.
	connLock, exists := writer.acquireCurrentConnectionLock(connectionName, false)
	if !exists {
		t.Fatalf("connection %s not found", connectionName)
	}
	defer connLock.Unlock()
	writer.mu.Lock()
	conn := writer.openConnections[connectionName]
	writer.mu.Unlock()
	conn.stopAdmissionCleanup()
	update(&conn)
	writer.scheduleAdmissionCleanup(connectionName, &conn)
	writer.mu.Lock()
	writer.openConnections[connectionName] = conn
	writer.mu.Unlock()
}

func admissionPayload(t *testing.T, name string) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(exportutil.ExportMsg{
		ID:           "request-" + name,
		EventType:    exportutil.AdmissionViolationEventType,
		Message:      "denied",
		ResourceName: name,
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return payload
}

func TestAdmissionPublishRotatesReadyJSONL(t *testing.T) {
	path := t.TempDir()
	writer := newAdmissionWriter()
	createAdmissionConnection(t, writer, "admission", path, 1)
	if err := writer.Publish(context.Background(), "admission", admissionPayload(t, "pod-1"), "admission"); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	files, err := os.ReadDir(filepath.Join(path, "admission"))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(files) != 1 || filepath.Ext(files[0].Name()) != admissionReadyExtension {
		t.Fatalf("expected one ready admission file, got %v", files)
	}
	content, err := os.ReadFile(filepath.Join(path, "admission", files[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.HasSuffix(string(content), "\n") || !strings.Contains(string(content), `"resourceName":"pod-1"`) {
		t.Fatalf("unexpected JSONL content %q", content)
	}
}

func TestAdmissionRotationRetryCompletesWithoutTraffic(t *testing.T) {
	path := t.TempDir()
	writer := newAdmissionWriter()
	// Hooks are per Writer and must be set before the stream captures them.
	// Both rename attempts run under the per-connection lock, so the plain
	// counter below needs no additional synchronization.
	attempts := 0
	writer.renameAdmissionFile = func(oldPath, newPath string) error {
		attempts++
		if attempts == 1 {
			return os.ErrPermission
		}
		return os.Rename(oldPath, newPath)
	}
	writer.admissionRotationRetryInterval = 10 * time.Millisecond
	createAdmissionConnection(t, writer, "admission", path, 100)
	if err := writer.Publish(context.Background(), "admission", admissionPayload(t, "pod-1"), "admission"); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	writer.mu.Lock()
	conn := writer.openConnections["admission"]
	stream := conn.admission.stream
	writer.mu.Unlock()
	if err := writer.rotateAdmissionStream("admission", "admission", stream.openPath); err == nil {
		t.Fatal("expected initial rotation failure")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		files, _ := os.ReadDir(filepath.Join(path, "admission"))
		connLock, exists := writer.acquireCurrentConnectionLock("admission", false)
		pending := true
		if exists {
			writer.mu.Lock()
			conn = writer.openConnections["admission"]
			writer.mu.Unlock()
			pending = conn.admission.stream != nil
			connLock.Unlock()
		}
		if len(files) == 1 && filepath.Ext(files[0].Name()) == admissionReadyExtension && !pending {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for rotation retry")
}

func TestAuditAndAdmissionShareConnectionWithoutMixingFiles(t *testing.T) {
	path := t.TempDir()
	writer := newAdmissionWriter()
	createAdmissionConnection(t, writer, "shared", path, 1)
	if err := writer.Publish(context.Background(), "shared", exportutil.ExportMsg{ID: "audit-1", Message: exportutil.AuditStartedMsg}, "audit"); err != nil {
		t.Fatalf("Publish(audit start) error = %v", err)
	}
	if err := writer.Publish(context.Background(), "shared", admissionPayload(t, "pod-1"), "audit"); err != nil {
		t.Fatalf("Publish(admission) error = %v", err)
	}
	if err := writer.Publish(context.Background(), "shared", exportutil.ExportMsg{ID: "audit-1", Message: "audit violation"}, "audit"); err != nil {
		t.Fatalf("Publish(audit violation) error = %v", err)
	}
	if err := writer.Publish(context.Background(), "shared", exportutil.ExportMsg{ID: "audit-1", Message: exportutil.AuditCompletedMsg}, "audit"); err != nil {
		t.Fatalf("Publish(audit end) error = %v", err)
	}

	auditData, err := os.ReadFile(filepath.Join(path, "audit", "audit-1.log"))
	if err != nil {
		t.Fatalf("ReadFile(audit) error = %v", err)
	}
	if strings.Contains(string(auditData), exportutil.AdmissionViolationEventType) {
		t.Fatal("admission record leaked into audit file")
	}
	files, err := os.ReadDir(filepath.Join(path, "audit"))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected distinct audit and admission files, got %v", files)
	}
	var auditFile, admissionFile bool
	for _, file := range files {
		auditFile = auditFile || file.Name() == "audit-1.log"
		admissionFile = admissionFile || strings.HasPrefix(file.Name(), admissionFilePrefix)
	}
	if !auditFile || !admissionFile {
		t.Fatalf("expected audit-1.log and %s*.log, got %v", admissionFilePrefix, files)
	}
}

func TestAdmissionTimerRotation(t *testing.T) {
	path := t.TempDir()
	writer := newAdmissionWriter()
	createAdmissionConnection(t, writer, "admission", path, 100)
	updateAdmissionTestConnection(t, writer, "admission", func(conn *Connection) { conn.admission.limits.maxFileAge = time.Second })
	if err := writer.Publish(context.Background(), "admission", admissionPayload(t, "pod-1"), "admission"); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		files, _ := os.ReadDir(filepath.Join(path, "admission"))
		if len(files) == 1 && filepath.Ext(files[0].Name()) == admissionReadyExtension {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for timer rotation")
}

func TestAdmissionRecoveryTruncatesPartialRecord(t *testing.T) {
	path := t.TempDir()
	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	openPath := filepath.Join(topicDir, admissionFilePrefix+"crash"+admissionOpenExtension)
	if err := os.WriteFile(openPath, []byte(completeAdmissionRecord+"{\"partial\":"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	writer := newAdmissionWriter()
	if err := writer.CreateConnection(context.Background(), "admission", admissionDiskConfig(path)); err != nil {
		t.Fatalf("CreateConnection() error = %v", err)
	}
	t.Cleanup(func() { _ = writer.CloseConnection("admission") })
	readyPath := strings.TrimSuffix(openPath, admissionOpenExtension) + ".recovered" + admissionReadyExtension
	content, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatalf("ReadFile(recovered) error = %v", err)
	}
	if string(content) != completeAdmissionRecord {
		t.Fatalf("unexpected recovered content %q", content)
	}
}

func TestRollbackAdmissionWriteRemovesPartialRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.open")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer file.Close()
	complete := []byte(completeAdmissionRecord)
	if _, err := file.Write(append(complete, []byte("{\"partial\":")...)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	stream := &admissionStream{file: file}
	if err := rollbackAdmissionWrite(stream, int64(len(complete))); err != nil {
		t.Fatalf("rollbackAdmissionWrite() error = %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != completeAdmissionRecord {
		t.Fatalf("unexpected rolled back content %q", content)
	}
}

func TestFinalizePoisonedAdmissionStreamRecoversCompleteRecords(t *testing.T) {
	dir := t.TempDir()
	openPath := filepath.Join(dir, admissionFilePrefix+"poisoned"+admissionOpenExtension)
	file, err := os.OpenFile(openPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := file.WriteString(completeAdmissionRecord + "{\"partial\":"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	stream := &admissionStream{file: file, openPath: openPath, poisoned: true, maxBytes: 4096}
	if err := finalizeAdmissionStream(stream); err != nil {
		t.Fatalf("finalizeAdmissionStream() error = %v", err)
	}
	recovered := strings.TrimSuffix(openPath, admissionOpenExtension) + ".recovered" + admissionReadyExtension
	content, err := os.ReadFile(recovered)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != completeAdmissionRecord {
		t.Fatalf("unexpected recovered content %q", content)
	}
}

func TestAdmissionPublishRecoversPoisonedStreamBeforeAppend(t *testing.T) {
	path := t.TempDir()
	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	openPath := filepath.Join(topicDir, admissionFilePrefix+"poisoned"+admissionOpenExtension)
	file, err := os.OpenFile(openPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	poisonedContent := completeAdmissionRecord + "{\"partial\":"
	if _, err := file.WriteString(poisonedContent); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("Flock() error = %v", err)
	}

	writer := newAdmissionWriter()
	conn := connectionFromConfig(&connectionConfig{path: path, closedConnectionTTL: time.Minute})
	conn.admission.limits.maxFileBytes = 4096
	conn.admission.limits.maxFileRecords = 100
	conn.admission.limits.maxTotalBytes = 8192
	conn.admission.limits.maxRecordBytes = 2048
	conn.admission.limits.minFreeBytes = 0
	conn.admission.stream = &admissionStream{
		file:     file,
		openPath: openPath,
		topic:    "admission",
		openedAt: time.Now(),
		bytes:    int64(len(poisonedContent)),
		records:  1,
		maxBytes: 4096,
		poisoned: true,
		rename:   os.Rename,
	}
	writer.openConnections["admission"] = conn
	t.Cleanup(func() { _ = writer.CloseConnection("admission") })

	if err := writer.Publish(context.Background(), "admission", admissionPayload(t, "next"), "admission"); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	recoveredPath := strings.TrimSuffix(openPath, admissionOpenExtension) + ".recovered" + admissionReadyExtension
	recovered, err := os.ReadFile(recoveredPath)
	if err != nil {
		t.Fatalf("ReadFile(recovered) error = %v", err)
	}
	if string(recovered) != completeAdmissionRecord {
		t.Fatalf("unexpected recovered content %q", recovered)
	}

	writer.mu.Lock()
	current := writer.openConnections["admission"].admission.stream
	writer.mu.Unlock()
	if current == nil || current.openPath == openPath || current.poisoned {
		t.Fatalf("expected a fresh healthy stream, got %#v", current)
	}
	currentContent, err := os.ReadFile(current.openPath)
	if err != nil {
		t.Fatalf("ReadFile(current) error = %v", err)
	}
	var message exportutil.ExportMsg
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(currentContent))), &message); err != nil {
		t.Fatalf("current stream contains invalid JSONL %q: %v", currentContent, err)
	}
	if message.ResourceName != "next" {
		t.Fatalf("current stream resource name = %q, want next", message.ResourceName)
	}
}

func TestAdmissionPublishRejectsAppendWhenPoisonedRecoveryFails(t *testing.T) {
	path := t.TempDir()
	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	openPath := filepath.Join(topicDir, admissionFilePrefix+"poisoned"+admissionOpenExtension)
	file, err := os.OpenFile(openPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	poisonedContent := completeAdmissionRecord + "{\"partial\":"
	if _, err := file.WriteString(poisonedContent); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	writer := newAdmissionWriter()
	conn := connectionFromConfig(&connectionConfig{path: path, closedConnectionTTL: time.Minute})
	conn.admission.limits.maxFileBytes = 4096
	conn.admission.limits.maxFileRecords = 100
	conn.admission.limits.maxTotalBytes = 8192
	conn.admission.limits.maxRecordBytes = 2048
	conn.admission.limits.minFreeBytes = 0
	conn.admission.stream = &admissionStream{
		file:     file,
		openPath: openPath,
		topic:    "admission",
		openedAt: time.Now(),
		bytes:    int64(len(poisonedContent)),
		records:  1,
		maxBytes: 4096,
		poisoned: true,
	}
	writer.openConnections["admission"] = conn
	t.Cleanup(func() { _ = writer.CloseConnection("admission") })

	err = writer.Publish(context.Background(), "admission", admissionPayload(t, "next"), "admission")
	if err == nil || !strings.Contains(err.Error(), "recovering poisoned admission stream") {
		t.Fatalf("expected poisoned recovery error, got %v", err)
	}
	content, readErr := os.ReadFile(openPath)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(content) != poisonedContent {
		t.Fatalf("poisoned content changed after rejected append: %q", content)
	}
	entries, readErr := os.ReadDir(topicDir)
	if readErr != nil {
		t.Fatalf("ReadDir() error = %v", readErr)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(openPath) {
		t.Fatalf("unexpected files after rejected append: %v", entries)
	}
}

func TestAdmissionRetentionBoundsFileCount(t *testing.T) {
	path := t.TempDir()
	writer := newAdmissionWriter()
	createAdmissionConnection(t, writer, "admission", path, 1)
	// maxResults is 2 and the byte budget is generous, so only the count binds.
	for _, name := range []string{"pod-1", "pod-2", "pod-3"} {
		if err := writer.Publish(context.Background(), "admission", admissionPayload(t, name), "admission"); err != nil {
			t.Fatalf("Publish(%s) error = %v", name, err)
		}
	}
	files, err := os.ReadDir(filepath.Join(path, "admission"))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected the count limit to keep two files, got %d", len(files))
	}
}

func TestAdmissionRetentionBoundsTotalBytes(t *testing.T) {
	path := t.TempDir()
	writer := newAdmissionWriter()
	createAdmissionConnection(t, writer, "admission", path, 1)
	// Measure a real segment, then budget room for exactly two of them and raise
	// the count limit so only the byte limit can evict.
	if err := writer.Publish(context.Background(), "admission", admissionPayload(t, "pod-0"), "admission"); err != nil {
		t.Fatalf("Publish(pod-0) error = %v", err)
	}
	segment := admissionReadyBytes(t, path)
	if segment <= 0 {
		t.Fatalf("expected a non-empty admission segment, got %d", segment)
	}
	updateAdmissionTestConnection(t, writer, "admission", func(conn *Connection) {
		conn.admission.limits.maxResults = 10
		conn.admission.limits.maxTotalBytes = 2 * segment
	})
	for _, name := range []string{"pod-1", "pod-2", "pod-3"} {
		if err := writer.Publish(context.Background(), "admission", admissionPayload(t, name), "admission"); err != nil {
			t.Fatalf("Publish(%s) error = %v", name, err)
		}
	}
	files, err := os.ReadDir(filepath.Join(path, "admission"))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected the byte limit to keep two files, got %d", len(files))
	}
}

func admissionReadyBytes(t *testing.T, path string) int64 {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(path, "admission"))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	var total int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("Info() error = %v", err)
		}
		total += info.Size()
	}
	return total
}

func TestAdmissionCleanupUsesSingleSortedSnapshot(t *testing.T) {
	path := t.TempDir()
	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	baseTime := time.Now().Add(-time.Hour)
	for i := range 5 {
		filePath := filepath.Join(topicDir, fmt.Sprintf("%s%02d%s", admissionFilePrefix, i, admissionReadyExtension))
		if err := os.WriteFile(filePath, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		modTime := baseTime.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(filePath, modTime, modTime); err != nil {
			t.Fatalf("Chtimes() error = %v", err)
		}
	}

	scanCalls := 0
	scan := func(root string) (admissionFileSummary, error) {
		scanCalls++
		return scanAdmissionReadyFiles(root)
	}

	conn := connectionFromConfig(&connectionConfig{path: path, closedConnectionTTL: time.Minute})
	conn.admission.limits.maxResults = 2
	conn.admission.limits.maxTotalBytes = 8192
	conn.admission.limits.fileTTL = 24 * time.Hour
	conn.admission.limits.minFreeBytes = 0
	if err := conn.cleanupAdmissionFilesWithScanner(0, scan); err != nil {
		t.Fatalf("cleanupAdmissionFiles() error = %v", err)
	}
	if scanCalls != 1 {
		t.Fatalf("expected one admission file scan, got %d", scanCalls)
	}
	entries, err := os.ReadDir(topicDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 2 || entries[0].Name() != admissionFilePrefix+"03"+admissionReadyExtension || entries[1].Name() != admissionFilePrefix+"04"+admissionReadyExtension {
		t.Fatalf("expected two newest admission files, got %v", entries)
	}
}

func TestAdmissionCleanupStopsAtRemovalLimit(t *testing.T) {
	path := t.TempDir()
	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for i := 0; i < maxAdmissionFilesPerCleanup+1; i++ {
		filePath := filepath.Join(topicDir, fmt.Sprintf("%s%03d%s", admissionFilePrefix, i, admissionReadyExtension))
		if err := os.WriteFile(filePath, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	conn := connectionFromConfig(&connectionConfig{path: path, closedConnectionTTL: time.Minute})
	conn.admission.limits.maxResults = 0
	conn.admission.limits.maxTotalBytes = 8192
	conn.admission.limits.fileTTL = 24 * time.Hour
	conn.admission.limits.minFreeBytes = 0
	err := conn.cleanupAdmissionFiles()
	if err == nil || !strings.Contains(err.Error(), "cleanup backlog") {
		t.Fatalf("expected cleanup backlog error, got %v", err)
	}
	entries, readErr := os.ReadDir(topicDir)
	if readErr != nil {
		t.Fatalf("ReadDir() error = %v", readErr)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one file after bounded cleanup, got %d", len(entries))
	}
}

func TestScanAdmissionReadyFilesBoundsRemovalCandidates(t *testing.T) {
	path := t.TempDir()
	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	fileCount := maxAdmissionFilesPerCleanup + 32
	baseTime := time.Now().Add(-time.Hour)
	for i := range fileCount {
		filePath := filepath.Join(topicDir, fmt.Sprintf("%s%03d%s", admissionFilePrefix, i, admissionReadyExtension))
		if err := os.WriteFile(filePath, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		modTime := baseTime.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(filePath, modTime, modTime); err != nil {
			t.Fatalf("Chtimes() error = %v", err)
		}
	}

	summary, err := scanAdmissionReadyFiles(path)
	if err != nil {
		t.Fatalf("scanAdmissionReadyFiles() error = %v", err)
	}
	if summary.totalFiles != fileCount {
		t.Fatalf("total files = %d, want %d", summary.totalFiles, fileCount)
	}
	if summary.totalBytes != int64(fileCount*len("{}\n")) {
		t.Fatalf("total bytes = %d, want %d", summary.totalBytes, fileCount*len("{}\n"))
	}
	if len(summary.files) != maxAdmissionFilesPerCleanup {
		t.Fatalf("cleanup candidates = %d, want %d", len(summary.files), maxAdmissionFilesPerCleanup)
	}
	for i, file := range summary.files {
		want := filepath.Join(topicDir, fmt.Sprintf("%s%03d%s", admissionFilePrefix, i, admissionReadyExtension))
		if file.path != want {
			t.Fatalf("cleanup candidate %d = %s, want %s", i, file.path, want)
		}
	}
	if !summary.hasNextOldest || !summary.nextOldest.modTime.Equal(baseTime.Add(maxAdmissionFilesPerCleanup*time.Second)) {
		t.Fatalf("next oldest file = %#v", summary.nextOldest)
	}
}

func TestAdmissionReadyFileCollectorKeepsOldestCandidatesRegardlessOfInputOrder(t *testing.T) {
	fileCount := maxAdmissionFilesPerCleanup + 64
	baseTime := time.Now().Add(-time.Hour)
	allFiles := make([]admissionReadyFile, fileCount)
	for index := range allFiles {
		allFiles[index] = admissionReadyFile{
			path:    fmt.Sprintf("topic-%d/%s%03d%s", index%3, admissionFilePrefix, index, admissionReadyExtension),
			size:    int64(index + 1),
			modTime: baseTime.Add(time.Duration(index/2) * time.Second),
		}
	}
	want := append([]admissionReadyFile(nil), allFiles...)
	sort.Slice(want, func(left, right int) bool {
		return admissionReadyFileBefore(want[left], want[right])
	})

	var collector admissionReadyFileCollector
	for offset := range fileCount {
		// This permutation visits every index and mixes old/new and equal-time files.
		collector.add(allFiles[(offset*67)%fileCount])
	}
	summary := collector.summary()
	if !slices.Equal(summary.files, want[:maxAdmissionFilesPerCleanup]) {
		t.Fatal("collector did not retain the globally oldest cleanup candidates")
	}
	if !summary.hasNextOldest || summary.nextOldest != want[maxAdmissionFilesPerCleanup] {
		t.Fatalf("next oldest file = %#v, want %#v", summary.nextOldest, want[maxAdmissionFilesPerCleanup])
	}
	if summary.totalFiles != fileCount {
		t.Fatalf("total files = %d, want %d", summary.totalFiles, fileCount)
	}
	var wantBytes int64
	for _, file := range allFiles {
		wantBytes += file.size
	}
	if summary.totalBytes != wantBytes {
		t.Fatalf("total bytes = %d, want %d", summary.totalBytes, wantBytes)
	}
}

func TestAdmissionCleanupReportsAgeBacklogAfterCandidateLimit(t *testing.T) {
	path := t.TempDir()
	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	oldTime := time.Now().Add(-time.Hour)
	for i := 0; i < maxAdmissionFilesPerCleanup+1; i++ {
		filePath := filepath.Join(topicDir, fmt.Sprintf("%s%03d%s", admissionFilePrefix, i, admissionReadyExtension))
		if err := os.WriteFile(filePath, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if err := os.Chtimes(filePath, oldTime, oldTime); err != nil {
			t.Fatalf("Chtimes() error = %v", err)
		}
	}

	conn := connectionFromConfig(&connectionConfig{path: path, closedConnectionTTL: time.Minute})
	conn.admission.limits.maxResults = maxAdmissionFilesPerCleanup + 1
	conn.admission.limits.maxTotalBytes = 8192
	conn.admission.limits.fileTTL = time.Minute
	conn.admission.limits.minFreeBytes = 0
	err := conn.cleanupAdmissionFiles()
	if err == nil || !strings.Contains(err.Error(), "cleanup backlog") {
		t.Fatalf("expected age cleanup backlog error, got %v", err)
	}
	entries, readErr := os.ReadDir(topicDir)
	if readErr != nil {
		t.Fatalf("ReadDir() error = %v", readErr)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one expired file after bounded cleanup, got %d", len(entries))
	}
}

// An overflow-saturated running total must stay above the limit so cleanup keeps
// draining instead of stopping early on a bogus byte count.
func TestAdmissionCleanupKeepsSaturatedTotalOverLimit(t *testing.T) {
	path := t.TempDir()
	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for i := range 3 {
		filePath := filepath.Join(topicDir, fmt.Sprintf("%s%02d%s", admissionFilePrefix, i, admissionReadyExtension))
		if err := os.WriteFile(filePath, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}
	scan := func(root string) (admissionFileSummary, error) {
		summary, err := scanAdmissionReadyFiles(root)
		if err != nil {
			return summary, err
		}
		summary.totalBytes = math.MaxInt64
		return summary, nil
	}

	conn := connectionFromConfig(&connectionConfig{path: path, closedConnectionTTL: time.Minute})
	// Only the byte limit may bind. The budget sits just under the saturated
	// total so subtracting file sizes would drop under it after two removals,
	// which is exactly what the guard must prevent.
	conn.admission.limits.maxResults = 10
	conn.admission.limits.maxTotalBytes = math.MaxInt64 - 4
	conn.admission.limits.fileTTL = 24 * time.Hour
	conn.admission.limits.minFreeBytes = 0
	if err := conn.cleanupAdmissionFilesWithScanner(0, scan); err != nil {
		t.Fatalf("cleanupAdmissionFilesWithScanner() error = %v", err)
	}
	entries, err := os.ReadDir(topicDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected the saturated total to drain every file, got %d", len(entries))
	}
}

func TestRecoverAdmissionTopicReadsAllBatches(t *testing.T) {
	path := t.TempDir()
	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for i := 0; i < admissionReadDirBatchSize+1; i++ {
		filePath := filepath.Join(topicDir, fmt.Sprintf("%s%03d%s", admissionFilePrefix, i, admissionDeletingExtension))
		if err := os.WriteFile(filePath, nil, 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	conn := connectionFromConfig(&connectionConfig{path: path, closedConnectionTTL: time.Minute})
	if err := conn.recoverAdmissionTopic("admission"); err != nil {
		t.Fatalf("recoverAdmissionTopic() error = %v", err)
	}
	entries, err := os.ReadDir(topicDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected recovery to process every directory batch, got %d files", len(entries))
	}
}

func TestAdmissionCleanupBackpressuresWhileOldestFileIsRead(t *testing.T) {
	path := t.TempDir()
	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	oldest := filepath.Join(topicDir, admissionFilePrefix+"old"+admissionReadyExtension)
	newest := filepath.Join(topicDir, admissionFilePrefix+"new"+admissionReadyExtension)
	for _, file := range []string{oldest, newest} {
		if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}
	// Backdate the locked file so it is the eviction candidate. Without this the
	// path tiebreak would sort "new" first and cleanup would evict an unlocked file.
	oldTime := time.Now().Add(-time.Minute)
	if err := os.Chtimes(oldest, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
	reader, err := os.Open(oldest)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reader.Close()
	if err := syscall.Flock(int(reader.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("Flock() error = %v", err)
	}
	released := false
	releaseReader := func() error {
		if released {
			return nil
		}
		released = true
		return syscall.Flock(int(reader.Fd()), syscall.LOCK_UN)
	}
	defer func() { _ = releaseReader() }()

	conn := connectionFromConfig(&connectionConfig{path: path, closedConnectionTTL: time.Minute})
	conn.admission.limits.maxResults = 1
	conn.admission.limits.maxFileBytes = 4096
	conn.admission.limits.maxFileRecords = 10
	conn.admission.limits.maxFileAge = time.Minute
	conn.admission.limits.maxTotalBytes = 8192
	conn.admission.limits.fileTTL = time.Hour
	conn.admission.limits.maxRecordBytes = 2048
	conn.admission.limits.minFreeBytes = 0

	err = conn.cleanupAdmissionFiles()
	// Match the sentinel rather than only the text, so rewording the message
	// cannot silently turn this into an any-error assertion.
	if !errors.Is(err, errAdmissionFileBusy) {
		t.Fatalf("expected errAdmissionFileBusy, got %v", err)
	}
	if !strings.Contains(err.Error(), "active reader") {
		t.Fatalf("expected an operator-facing active reader message, got %v", err)
	}
	// Backpressure must leave the spool byte-for-byte intact: the locked file is
	// not deleted, no .deleting remnant is left by a half-finished two-phase
	// removal, and cleanup does not skip ahead to evict the newer file.
	assertAdmissionDirContains(t, topicDir, filepath.Base(newest), filepath.Base(oldest))

	// Releasing the reader must let the very same cleanup make progress. This is
	// the control: it proves the lock caused the failure rather than some
	// unrelated permanent error that would make the assertions above vacuous.
	if err := releaseReader(); err != nil {
		t.Fatalf("Flock(LOCK_UN) error = %v", err)
	}
	if err := conn.cleanupAdmissionFiles(); err != nil {
		t.Fatalf("cleanupAdmissionFiles() after releasing reader error = %v", err)
	}
	assertAdmissionDirContains(t, topicDir, filepath.Base(newest))
}

func assertAdmissionDirContains(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	// os.ReadDir already returns entries sorted by name.
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected admission files: got %v, want %v", got, want)
	}
}

func TestAdmissionPeriodicCleanupRemovesExpiredIdleFile(t *testing.T) {
	path := t.TempDir()
	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	readyPath := filepath.Join(topicDir, admissionFilePrefix+"expired"+admissionReadyExtension)
	if err := os.WriteFile(readyPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	oldTime := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(readyPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	writer := newAdmissionWriter()
	createAdmissionConnection(t, writer, "admission", path, 100)
	updateAdmissionTestConnection(t, writer, "admission", func(conn *Connection) {
		conn.admission.limits.maxFileAge = time.Second
		conn.admission.limits.fileTTL = time.Second
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(readyPath); os.IsNotExist(err) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for periodic admission cleanup")
}

func TestAdmissionPeriodicCleanupRecoversAbandonedOpenFile(t *testing.T) {
	path := t.TempDir()
	writer := newAdmissionWriter()
	createAdmissionConnection(t, writer, "admission", path, 100)
	updateAdmissionTestConnection(t, writer, "admission", func(conn *Connection) { conn.admission.limits.maxFileAge = time.Second })

	topicDir := filepath.Join(path, "admission")
	if err := os.MkdirAll(topicDir, 0o770); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	openPath := filepath.Join(topicDir, admissionFilePrefix+"abandoned"+admissionOpenExtension)
	if err := os.WriteFile(openPath, []byte(completeAdmissionRecord+"{\"partial\":"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	recoveredPath := strings.TrimSuffix(openPath, admissionOpenExtension) + ".recovered" + admissionReadyExtension

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(recoveredPath)
		if err == nil {
			if string(content) != completeAdmissionRecord {
				t.Fatalf("unexpected recovered content %q", content)
			}
			return
		}
		if !os.IsNotExist(err) {
			t.Fatalf("ReadFile() error = %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for periodic admission recovery")
}

func TestAdmissionFreeSpaceReserveRejectsWrite(t *testing.T) {
	path := t.TempDir()
	writer := newAdmissionWriter()
	createAdmissionConnection(t, writer, "admission", path, 100)
	updateAdmissionTestConnection(t, writer, "admission", func(conn *Connection) {
		conn.admission.limits.minFreeBytes = math.MaxInt64
	})
	err := writer.Publish(context.Background(), "admission", admissionPayload(t, "pod-1"), "admission")
	if err == nil || !strings.Contains(err.Error(), "insufficient admission disk space") {
		t.Fatalf("expected free-space error, got %v", err)
	}
}

func TestCloseConnectionStopsAdmissionJanitor(t *testing.T) {
	path := t.TempDir()
	writer := newAdmissionWriter()
	createAdmissionConnection(t, writer, "admission", path, 100)
	writer.mu.Lock()
	timer := writer.openConnections["admission"].admission.cleanupTimer
	writer.mu.Unlock()
	if timer == nil {
		t.Fatal("expected admission cleanup timer")
	}
	if err := writer.CloseConnection("admission"); err != nil {
		t.Fatalf("CloseConnection() error = %v", err)
	}
	if timer.Stop() {
		t.Fatal("cleanup timer was still active after CloseConnection")
	}
}

func TestAdmissionRejectsSymlinkTopic(t *testing.T) {
	path := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(path, "admission")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	writer := newAdmissionWriter()
	createAdmissionConnection(t, writer, "admission", path, 100)
	err := writer.Publish(context.Background(), "admission", admissionPayload(t, "pod-1"), "admission")
	if err == nil || !strings.Contains(err.Error(), "not a symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestIndependentWritersUseUniqueAdmissionFiles(t *testing.T) {
	path := t.TempDir()
	writers := []*Writer{newAdmissionWriter(), newAdmissionWriter()}
	for index, writer := range writers {
		connectionName := "admission-" + string(rune('a'+index))
		createAdmissionConnection(t, writer, connectionName, path, 1)
		if err := writer.Publish(context.Background(), connectionName, admissionPayload(t, connectionName), "admission"); err != nil {
			t.Fatalf("Publish(%s) error = %v", connectionName, err)
		}
	}
	files, err := os.ReadDir(filepath.Join(path, "admission"))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected two admission files, got %v", files)
	}
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), admissionFilePrefix) || filepath.Ext(file.Name()) != admissionReadyExtension {
			t.Fatalf("unexpected admission file %q", file.Name())
		}
	}
}
