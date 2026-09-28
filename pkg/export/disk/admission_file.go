package disk

import (
	"container/heap"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	admissionFilePrefix        = "admission-"
	admissionOpenExtension     = ".open"
	admissionReadyExtension    = ".log"
	admissionDeletingExtension = ".deleting"
	// Bound filesystem mutations performed by one publish or janitor pass.
	maxAdmissionFilesPerCleanup = 256
	// Bound temporary allocation while still exhausting each directory.
	admissionReadDirBatchSize   = 128
	maxAdmissionCleanupInterval = time.Minute
)

var errAdmissionFileBusy = errors.New("admission file is busy")

const defaultAdmissionRotationRetryInterval = 30 * time.Second

// admissionStream owns one locked JSONL segment from creation through atomic
// publication as a completed .log file.
type admissionStream struct {
	file     *os.File
	openPath string
	topic    string
	openedAt time.Time
	bytes    int64
	records  int64
	timer    *time.Timer
	maxBytes int64
	poisoned bool
	// rename is captured from the owning Writer so publication stays injectable
	// on paths that finalize a stream without a Writer in scope.
	rename func(oldPath, newPath string) error
	sync   func(*os.File) error
}

// renameFile falls back to os.Rename so a stream built outside a Writer still
// publishes normally.
func (stream *admissionStream) renameFile(oldPath, newPath string) error {
	if stream.rename == nil {
		return os.Rename(oldPath, newPath)
	}
	return stream.rename(oldPath, newPath)
}

func (stream *admissionStream) syncFile() error {
	if stream.file == nil {
		return nil
	}
	if stream.sync == nil {
		return stream.file.Sync()
	}
	return stream.sync(stream.file)
}

// admissionRename resolves the Writer's rename hook. Keeping this per Writer
// avoids mutating package state that background rotation timers read.
func (r *Writer) admissionRename() func(oldPath, newPath string) error {
	if r.renameAdmissionFile == nil {
		return os.Rename
	}
	return r.renameAdmissionFile
}

func (r *Writer) admissionSync() func(*os.File) error {
	if r.syncAdmissionFile == nil {
		return func(file *os.File) error { return file.Sync() }
	}
	return r.syncAdmissionFile
}

func (r *Writer) rotationRetryInterval() time.Duration {
	if r.admissionRotationRetryInterval <= 0 {
		return defaultAdmissionRotationRetryInterval
	}
	return r.admissionRotationRetryInterval
}

type admissionReadyFile struct {
	path    string
	size    int64
	modTime time.Time
}

type admissionFileSummary struct {
	files         []admissionReadyFile
	totalFiles    int
	totalBytes    int64
	nextOldest    admissionReadyFile
	hasNextOldest bool
}

func (summary *admissionFileSummary) oldestModTime() time.Time {
	if len(summary.files) > 0 {
		return summary.files[0].modTime
	}
	if summary.hasNextOldest {
		return summary.nextOldest.modTime
	}
	return time.Time{}
}

type admissionReadyFileMaxHeap []admissionReadyFile

func (files admissionReadyFileMaxHeap) Len() int { return len(files) }

func (files admissionReadyFileMaxHeap) Less(left, right int) bool {
	// Reverse oldest-first order so the root is the newest retained candidate.
	return admissionReadyFileBefore(files[right], files[left])
}

func (files admissionReadyFileMaxHeap) Swap(left, right int) {
	files[left], files[right] = files[right], files[left]
}

func (files *admissionReadyFileMaxHeap) Push(value any) {
	file, ok := value.(admissionReadyFile)
	if !ok {
		panic("invalid admission cleanup candidate")
	}
	*files = append(*files, file)
}

func (files *admissionReadyFileMaxHeap) Pop() any {
	previous := *files
	last := len(previous) - 1
	value := previous[last]
	*files = previous[:last]
	return value
}

type admissionReadyFileCollector struct {
	candidates    admissionReadyFileMaxHeap
	totalFiles    int
	totalBytes    int64
	nextOldest    admissionReadyFile
	hasNextOldest bool
}

func (collector *admissionReadyFileCollector) add(file admissionReadyFile) {
	if collector.totalFiles < math.MaxInt {
		collector.totalFiles++
	}
	if collector.totalBytes > math.MaxInt64-file.size {
		collector.totalBytes = math.MaxInt64
	} else {
		collector.totalBytes += file.size
	}

	if len(collector.candidates) < maxAdmissionFilesPerCleanup {
		heap.Push(&collector.candidates, file)
		return
	}
	if admissionReadyFileBefore(file, collector.candidates[0]) {
		excluded, ok := heap.Pop(&collector.candidates).(admissionReadyFile)
		if !ok {
			panic("invalid admission cleanup candidate")
		}
		collector.recordExcluded(excluded)
		heap.Push(&collector.candidates, file)
		return
	}
	collector.recordExcluded(file)
}

func (collector *admissionReadyFileCollector) recordExcluded(file admissionReadyFile) {
	if !collector.hasNextOldest || admissionReadyFileBefore(file, collector.nextOldest) {
		collector.nextOldest = file
		collector.hasNextOldest = true
	}
}

func (collector *admissionReadyFileCollector) summary() admissionFileSummary {
	files := append([]admissionReadyFile(nil), collector.candidates...)
	sort.Slice(files, func(left, right int) bool {
		return admissionReadyFileBefore(files[left], files[right])
	})
	return admissionFileSummary{
		files:         files,
		totalFiles:    collector.totalFiles,
		totalBytes:    collector.totalBytes,
		nextOldest:    collector.nextOldest,
		hasNextOldest: collector.hasNextOldest,
	}
}

func admissionReadyFileBefore(left, right admissionReadyFile) bool {
	if left.modTime.Equal(right.modTime) {
		return left.path < right.path
	}
	return left.modTime.Before(right.modTime)
}

// writeAdmissionRecord appends one durable JSONL record, rotating before or
// after the write as limits require. The caller holds the per-Connection lock
// and must persist the mutated Connection value even when this function fails.
func (r *Writer) writeAdmissionRecord(ctx context.Context, connectionName string, conn *Connection, topic string, data []byte) error {
	return r.writeAdmissionRecords(ctx, connectionName, conn, topic, [][]byte{data})
}

func (r *Writer) writeAdmissionRecords(ctx context.Context, connectionName string, conn *Connection, topic string, records [][]byte) error {
	if err := validatePathSegment("topic", topic); err != nil {
		return err
	}
	limits := conn.admission.limits
	var batchBytes int64
	for _, data := range records {
		if int64(len(data)) > limits.maxRecordBytes {
			return fmt.Errorf("admission record size %d exceeds maximum %d", len(data), limits.maxRecordBytes)
		}
		recordBytes := int64(len(data) + 1)
		if recordBytes > limits.maxFileBytes {
			return fmt.Errorf("admission record size %d exceeds file maximum %d", recordBytes, limits.maxFileBytes)
		}
		if batchBytes > math.MaxInt64-recordBytes {
			return fmt.Errorf("admission batch size overflow")
		}
		batchBytes += recordBytes
	}
	if err := conn.cleanupAdmissionFilesForWrite(batchBytes); err != nil {
		return err
	}
	if err := ensureAdmissionFreeSpace(conn.Path, limits.minFreeBytes, batchBytes); err != nil {
		if cleanupErr := conn.cleanupAdmissionFilesForWrite(batchBytes); cleanupErr != nil {
			return errors.Join(err, cleanupErr)
		}
		if err := ensureAdmissionFreeSpace(conn.Path, limits.minFreeBytes, batchBytes); err != nil {
			return err
		}
	}

	if stream := conn.admission.stream; stream != nil && stream.poisoned {
		if err := finalizeAdmissionStream(stream); err != nil {
			r.scheduleAdmissionRotationRetry(connectionName, topic, stream)
			return fmt.Errorf("recovering poisoned admission stream: %w", err)
		}
		conn.admission.stream = nil
		conn.admission.usageInitialized = false
		if err := conn.cleanupAdmissionFiles(); err != nil {
			return err
		}
	}

	rotated := false
	for _, data := range records {
		recordBytes := int64(len(data) + 1)
		stream := conn.admission.stream
		if stream == nil {
			var err error
			stream, err = r.openAdmissionStream(connectionName, conn, topic)
			if err != nil {
				return err
			}
			conn.admission.stream = stream
		} else if stream.records > 0 && (stream.bytes+recordBytes > limits.maxFileBytes || stream.records >= limits.maxFileRecords || time.Since(stream.openedAt) >= limits.maxFileAge) {
			if err := finalizeAdmissionStream(stream); err != nil {
				r.scheduleAdmissionRotationRetry(connectionName, topic, stream)
				return err
			}
			conn.admission.usageInitialized = false
			rotated = true
			conn.admission.stream = nil
			var err error
			stream, err = r.openAdmissionStream(connectionName, conn, topic)
			if err != nil {
				return err
			}
			conn.admission.stream = stream
		}

		if err := ctx.Err(); err != nil {
			return errors.Join(fmt.Errorf("publish canceled: %w", err), r.syncAdmissionBatch(connectionName, topic, conn))
		}
		record := make([]byte, len(data)+1)
		copy(record, data)
		record[len(data)] = '\n'
		originalBytes := stream.bytes
		written, err := stream.file.Write(record)
		if err != nil || written != len(record) {
			writeErr := err
			if writeErr == nil {
				writeErr = io.ErrShortWrite
			}
			rollbackErr := rollbackAdmissionWrite(stream, originalBytes)
			if rollbackErr != nil {
				stream.poisoned = true
				r.scheduleAdmissionRotationRetry(connectionName, topic, stream)
			}
			return errors.Join(fmt.Errorf("writing admission record: %w", writeErr), rollbackErr)
		}
		stream.bytes += int64(written)
		stream.records++

		if stream.bytes >= limits.maxFileBytes || stream.records >= limits.maxFileRecords {
			if err := finalizeAdmissionStream(stream); err != nil {
				r.scheduleAdmissionRotationRetry(connectionName, topic, stream)
				return err
			}
			conn.admission.usageInitialized = false
			rotated = true
			conn.admission.stream = nil
		}
	}
	if err := r.syncAdmissionBatch(connectionName, topic, conn); err != nil {
		return err
	}
	if conn.admission.cleanupTimer == nil && (conn.admission.stream != nil || rotated) {
		r.scheduleAdmissionCleanup(connectionName, conn)
	}
	if rotated {
		return conn.cleanupAdmissionFiles()
	}
	return nil
}

func (r *Writer) syncAdmissionBatch(connectionName, topic string, conn *Connection) error {
	stream := conn.admission.stream
	if stream == nil {
		return nil
	}
	if err := stream.syncFile(); err != nil {
		r.scheduleAdmissionRotationRetry(connectionName, topic, stream)
		return fmt.Errorf("syncing admission records: %w", err)
	}
	return nil
}

// openAdmissionStream uses a random O_EXCL name and file lock so independent
// writer processes sharing storage cannot claim the same segment.
func (r *Writer) openAdmissionStream(connectionName string, conn *Connection, topic string) (*admissionStream, error) {
	dir, err := auditDirPath(conn.Path, topic)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return nil, fmt.Errorf("creating admission directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("checking admission directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("admission topic path must be a directory and not a symlink: %s", dir)
	}
	if err := os.Chmod(dir, 0o770); err != nil {
		return nil, fmt.Errorf("setting admission directory permissions: %w", err)
	}

	var file *os.File
	var openPath string
	for range 8 {
		name, err := admissionFileName()
		if err != nil {
			return nil, err
		}
		openPath = filepath.Join(dir, name+admissionOpenExtension)
		file, err = os.OpenFile(openPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0o660)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("opening admission file: %w", err)
		}
	}
	if file == nil {
		return nil, fmt.Errorf("creating unique admission file")
	}
	if err := lockFile(file); err != nil {
		closeErr := file.Close()
		removeErr := os.Remove(openPath)
		return nil, fmt.Errorf("locking admission file: %w", errors.Join(err, closeErr, removeErr))
	}

	stream := &admissionStream{
		file:     file,
		openPath: openPath,
		topic:    topic,
		openedAt: time.Now(),
		maxBytes: conn.admission.limits.maxFileBytes,
		rename:   r.admissionRename(),
		sync:     r.admissionSync(),
	}
	stream.timer = time.AfterFunc(conn.admission.limits.maxFileAge, func() {
		if err := r.rotateAdmissionStream(connectionName, topic, openPath); err != nil {
			log.Error(err, "rotating admission export file", "connection", connectionName, "topic", topic)
		}
	})
	return stream, nil
}

func admissionFileName() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generating admission file ID: %w", err)
	}
	return admissionFilePrefix + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random), nil
}

// rotateAdmissionStream ignores stale timer callbacks by requiring the active
// stream to still match both the topic and expected path.
func (r *Writer) rotateAdmissionStream(connectionName, topic, expectedPath string) error {
	connLock, exists := r.acquireCurrentConnectionLock(connectionName, false)
	if !exists {
		return nil
	}
	defer connLock.Unlock()

	r.mu.Lock()
	conn, exists := r.openConnections[connectionName]
	r.mu.Unlock()
	if !exists {
		return nil
	}
	stream := conn.admission.stream
	if stream == nil || stream.topic != topic || stream.openPath != expectedPath {
		return nil
	}
	if stream.records == 0 {
		if err := closeAdmissionStream(stream); err != nil {
			r.scheduleAdmissionRotationRetry(connectionName, topic, stream)
			return err
		}
		if err := os.Remove(stream.openPath); err != nil && !os.IsNotExist(err) {
			r.scheduleAdmissionRotationRetry(connectionName, topic, stream)
			return fmt.Errorf("removing empty admission file: %w", err)
		}
	} else if err := finalizeAdmissionStream(stream); err != nil {
		r.scheduleAdmissionRotationRetry(connectionName, topic, stream)
		return err
	}
	conn.admission.usageInitialized = false
	conn.admission.stream = nil
	r.mu.Lock()
	if r.connectionLockIsCurrentLocked(connectionName, connLock) {
		r.openConnections[connectionName] = conn
	}
	r.mu.Unlock()
	return conn.cleanupAdmissionFiles()
}

// scheduleAdmissionRotationRetry retains stream ownership and retries without
// requiring another admission record to arrive.
func (r *Writer) scheduleAdmissionRotationRetry(connectionName, topic string, stream *admissionStream) {
	if stream == nil {
		return
	}
	if stream.timer != nil {
		stream.timer.Stop()
	}
	expectedPath := stream.openPath
	stream.timer = time.AfterFunc(r.rotationRetryInterval(), func() {
		if err := r.rotateAdmissionStream(connectionName, topic, expectedPath); err != nil {
			log.Error(err, "retrying admission export file rotation", "connection", connectionName, "topic", topic)
		}
	})
}

// finalizeAdmissionStream syncs and closes an open segment before atomically
// publishing it as .log. A poisoned stream is recovered only through its last
// complete newline-delimited record.
func finalizeAdmissionStream(stream *admissionStream) error {
	if stream.timer != nil {
		stream.timer.Stop()
		stream.timer = nil
	}
	if stream.poisoned {
		if err := closeAdmissionStream(stream); err != nil {
			return err
		}
		return recoverAdmissionOpenFile(stream.openPath, stream.maxBytes)
	}
	var syncErr error
	if stream.file != nil {
		syncErr = stream.syncFile()
		if closeErr := closeAdmissionStream(stream); closeErr != nil {
			return errors.Join(syncErr, closeErr)
		}
	}
	readyPath := strings.TrimSuffix(stream.openPath, admissionOpenExtension) + admissionReadyExtension
	if _, err := os.Stat(readyPath); err == nil {
		if _, openErr := os.Stat(stream.openPath); openErr == nil {
			return errors.Join(syncErr, fmt.Errorf("both admission open and ready files exist for %s", stream.openPath))
		} else if !os.IsNotExist(openErr) {
			return errors.Join(syncErr, fmt.Errorf("checking admission open file: %w", openErr))
		}
		if chmodErr := os.Chmod(readyPath, 0o660); chmodErr != nil {
			return errors.Join(syncErr, fmt.Errorf("setting admission file permissions: %w", chmodErr))
		}
		return syncErr
	} else if !os.IsNotExist(err) {
		return errors.Join(syncErr, fmt.Errorf("checking admission ready file: %w", err))
	}
	if err := stream.renameFile(stream.openPath, readyPath); err != nil {
		return errors.Join(syncErr, fmt.Errorf("renaming admission file: %w", err))
	}
	if err := os.Chmod(readyPath, 0o660); err != nil {
		return errors.Join(syncErr, fmt.Errorf("setting admission file permissions: %w", err))
	}
	return syncErr
}

// rollbackAdmissionWrite restores the last complete JSONL boundary after a
// partial or short write.
func rollbackAdmissionWrite(stream *admissionStream, size int64) error {
	truncateErr := stream.file.Truncate(size)
	_, seekErr := stream.file.Seek(0, io.SeekEnd)
	syncErr := stream.syncFile()
	return errors.Join(truncateErr, seekErr, syncErr)
}

func closeAdmissionStream(stream *admissionStream) error {
	if stream == nil {
		return nil
	}
	if stream.timer != nil {
		stream.timer.Stop()
		stream.timer = nil
	}
	if stream.file == nil {
		return nil
	}
	fd := int(stream.file.Fd())
	var unlockErr error
	if fd >= 0 {
		unlockErr = syscall.Flock(fd, syscall.LOCK_UN)
	}
	closeErr := stream.file.Close()
	if closeErr == nil || errors.Is(closeErr, os.ErrClosed) {
		stream.file = nil
	}
	return errors.Join(unlockErr, closeErr)
}

func (conn *Connection) closeAdmissionStream(finalize bool) error {
	conn.stopAdmissionCleanup()
	stream := conn.admission.stream
	if stream == nil {
		return nil
	}
	var err error
	if finalize && stream.records > 0 {
		err = finalizeAdmissionStream(stream)
	} else {
		err = closeAdmissionStream(stream)
	}
	if err != nil {
		return fmt.Errorf("closing admission channel %s: %w", stream.topic, err)
	}
	conn.admission.stream = nil
	return nil
}

func (conn *Connection) hasOpenFiles() bool {
	if conn.File != nil {
		return true
	}
	return conn.admission.stream != nil
}

func (conn *Connection) cleanupAdmissionFiles() error {
	return conn.cleanupAdmissionFilesWithReservation(0)
}

func (conn *Connection) cleanupAdmissionFilesForWrite(recordBytes int64) error {
	return conn.cleanupAdmissionFilesWithReservation(recordBytes)
}

// cleanupAdmissionFilesWithReservation removes oldest completed segments until
// the spool can accommodate reservedBytes. A reader lock causes backpressure
// rather than deletion of a file being consumed.
func (conn *Connection) cleanupAdmissionFilesWithReservation(reservedBytes int64) error {
	return conn.cleanupAdmissionFilesWithScanner(reservedBytes, scanAdmissionReadyFiles)
}

func (conn *Connection) cleanupAdmissionFilesWithScanner(reservedBytes int64, scan func(string) (admissionFileSummary, error)) error {
	limits := conn.admission.limits
	if reservedBytes > limits.maxTotalBytes {
		return fmt.Errorf("admission record exceeds total byte limit %d", limits.maxTotalBytes)
	}
	openBytes := conn.admissionOpenBytes()
	if openBytes > limits.maxTotalBytes-reservedBytes {
		return fmt.Errorf("admission open files and record exceed total byte limit %d", limits.maxTotalBytes)
	}
	maxReadyBytes := limits.maxTotalBytes - openBytes - reservedBytes
	if conn.admission.usageInitialized {
		expired := !conn.admission.oldestReadyTime.IsZero() && time.Since(conn.admission.oldestReadyTime) > limits.fileTTL
		if !expired && conn.admission.readyCount <= limits.maxResults && conn.admission.readyBytes <= maxReadyBytes {
			return nil
		}
	}

	summary, err := scan(conn.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer func() { conn.setAdmissionReadyUsage(&summary) }()
	now := time.Now()
	removed := 0
	for {
		oldestModTime := summary.oldestModTime()
		expired := !oldestModTime.IsZero() && now.Sub(oldestModTime) > limits.fileTTL
		withinCount := summary.totalFiles <= limits.maxResults
		withinBytes := summary.totalBytes <= maxReadyBytes
		// Retention is satisfied only when age, count, and byte limits all hold.
		if !expired && withinCount && withinBytes {
			return nil
		}
		if removed == maxAdmissionFilesPerCleanup || len(summary.files) == 0 {
			return fmt.Errorf("admission cleanup backlog remains above configured retention limits")
		}
		oldest := summary.files[0]
		if err := removeAdmissionReadyFile(oldest.path); err != nil {
			if errors.Is(err, errAdmissionFileBusy) {
				return fmt.Errorf("admission cleanup blocked by active reader: %w", err)
			}
			return err
		}
		summary.files = summary.files[1:]
		if summary.totalFiles != math.MaxInt {
			summary.totalFiles--
		}
		// Keep an overflow-saturated total over-limit rather than risk under-cleanup.
		if summary.totalFiles == 0 {
			summary.totalBytes = 0
			summary.hasNextOldest = false
		} else if summary.totalBytes != math.MaxInt64 {
			summary.totalBytes -= oldest.size
		}
		removed++
	}
}

func (conn *Connection) setAdmissionReadyUsage(summary *admissionFileSummary) {
	conn.admission.usageInitialized = true
	conn.admission.readyCount = summary.totalFiles
	conn.admission.readyBytes = summary.totalBytes
	conn.admission.oldestReadyTime = summary.oldestModTime()
}

func (conn *Connection) admissionOpenBytes() int64 {
	if conn.admission.stream == nil || conn.admission.stream.bytes <= 0 {
		return 0
	}
	return conn.admission.stream.bytes
}

// scanAdmissionReadyFiles returns exact aggregate usage and a bounded,
// deterministic oldest-first set of cleanup candidates across all topics.
func scanAdmissionReadyFiles(root string) (admissionFileSummary, error) {
	var collector admissionReadyFileCollector
	directory, err := os.Open(root)
	if err != nil {
		return admissionFileSummary{}, err
	}
	defer directory.Close()
	// ReadDir advances the directory cursor by one bounded batch per call.
	for {
		entries, err := directory.ReadDir(admissionReadDirBatchSize)
		if err != nil && !errors.Is(err, io.EOF) {
			return collector.summary(), err
		}
		for _, entry := range entries {
			if !entry.IsDir() || validatePathSegment("topic", entry.Name()) != nil {
				continue
			}
			if scanErr := scanAdmissionReadyFilesInDir(filepath.Join(root, entry.Name()), &collector); scanErr != nil {
				return collector.summary(), scanErr
			}
		}
		if errors.Is(err, io.EOF) {
			return collector.summary(), nil
		}
	}
}

func scanAdmissionReadyFilesInDir(dir string, collector *admissionReadyFileCollector) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	// ReadDir advances the directory cursor by one bounded batch per call.
	for {
		entries, err := directory.ReadDir(admissionReadDirBatchSize)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), admissionFilePrefix) || filepath.Ext(entry.Name()) != admissionReadyExtension {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				if os.IsNotExist(infoErr) {
					continue
				}
				return infoErr
			}
			path := filepath.Join(dir, entry.Name())
			collector.add(admissionReadyFile{
				path:    path,
				size:    info.Size(),
				modTime: info.ModTime(),
			})
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

// removeAdmissionReadyFile takes an exclusive non-blocking lock before renaming
// and deleting a completed segment. The rename prevents another scanner from
// selecting a file once deletion has begun.
func removeAdmissionReadyFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to remove admission symlink %s", path)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	fd := int(file.Fd())
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return errAdmissionFileBusy
		}
		return err
	}
	deletingPath := path + admissionDeletingExtension
	renameErr := os.Rename(path, deletingPath)
	unlockErr := syscall.Flock(fd, syscall.LOCK_UN)
	closeErr := file.Close()
	if renameErr != nil {
		return errors.Join(renameErr, unlockErr, closeErr)
	}
	removeErr := os.Remove(deletingPath)
	return errors.Join(unlockErr, closeErr, removeErr)
}

// recoverAdmissionFiles repairs abandoned admission files across channel
// directories, then applies retention. Files locked by a live writer are left
// untouched.
func (conn *Connection) recoverAdmissionFiles() (bool, error) {
	conn.admission.usageInitialized = false
	base, err := os.Open(conn.Path)
	if err != nil {
		return false, err
	}
	defer base.Close()
	for {
		entries, err := base.ReadDir(admissionReadDirBatchSize)
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || validatePathSegment("topic", entry.Name()) != nil {
				continue
			}
			if recoverErr := conn.recoverAdmissionTopic(entry.Name()); recoverErr != nil {
				return false, recoverErr
			}
		}
		if errors.Is(err, io.EOF) {
			if cleanupErr := conn.cleanupAdmissionFiles(); cleanupErr != nil {
				return false, cleanupErr
			}
			return hasAdmissionArtifacts(conn.Path)
		}
	}
}

func hasAdmissionArtifacts(root string) (bool, error) {
	directory, err := os.Open(root)
	if err != nil {
		return false, err
	}
	defer directory.Close()
	for {
		entries, readErr := directory.ReadDir(admissionReadDirBatchSize)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return false, readErr
		}
		for _, entry := range entries {
			if !entry.IsDir() || validatePathSegment("topic", entry.Name()) != nil {
				continue
			}
			topicDirectory, openErr := os.Open(filepath.Join(root, entry.Name()))
			if openErr != nil {
				return false, openErr
			}
			for {
				topicEntries, topicReadErr := topicDirectory.ReadDir(admissionReadDirBatchSize)
				if topicReadErr != nil && !errors.Is(topicReadErr, io.EOF) {
					_ = topicDirectory.Close()
					return false, topicReadErr
				}
				for _, topicEntry := range topicEntries {
					if !topicEntry.IsDir() && strings.HasPrefix(topicEntry.Name(), admissionFilePrefix) {
						_ = topicDirectory.Close()
						return true, nil
					}
				}
				if errors.Is(topicReadErr, io.EOF) {
					break
				}
			}
			if closeErr := topicDirectory.Close(); closeErr != nil {
				return false, closeErr
			}
		}
		if errors.Is(readErr, io.EOF) {
			return false, nil
		}
	}
}

func (conn *Connection) recoverAdmissionTopic(topic string) error {
	dir, err := auditDirPath(conn.Path, topic)
	if err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	// ReadDir advances the directory cursor by one bounded batch per call.
	for {
		entries, readErr := directory.ReadDir(admissionReadDirBatchSize)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), admissionFilePrefix) {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			switch {
			case strings.Contains(entry.Name(), admissionDeletingExtension):
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return err
				}
			case filepath.Ext(entry.Name()) == admissionOpenExtension:
				if err := recoverAdmissionOpenFile(path, conn.admission.limits.maxFileBytes); err != nil && !errors.Is(err, errAdmissionFileBusy) {
					return err
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
	}
}

// ensureAdmissionFreeSpace uses blocks available to the current user and keeps
// both the configured reserve and the incoming record outside that reserve.
func ensureAdmissionFreeSpace(path string, reserveBytes, recordBytes int64) error {
	if reserveBytes < 0 || recordBytes < 0 {
		return fmt.Errorf("admission disk space values cannot be negative")
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return fmt.Errorf("checking admission disk space: %w", err)
	}
	available := stat.Bavail
	if stat.Bsize <= 0 {
		return fmt.Errorf("invalid admission filesystem block size %d", stat.Bsize)
	}
	blockSize := uint64(stat.Bsize) // #nosec G115 -- positivity is checked above.
	if blockSize != 0 && available > math.MaxUint64/blockSize {
		available = math.MaxUint64
	} else {
		available *= blockSize
	}
	required := uint64(reserveBytes)  // #nosec G115 -- negativity is checked above.
	recordSize := uint64(recordBytes) // #nosec G115 -- negativity is checked above.
	if recordSize > math.MaxUint64-required {
		required = math.MaxUint64
	} else {
		required += recordSize
	}
	if available < required {
		return fmt.Errorf("insufficient admission disk space: available %d, required %d: %w", available, required, syscall.ENOSPC)
	}
	return nil
}

// stopAdmissionCleanup invalidates callbacks that already captured the previous
// cleanup generation, even when stopping the timer races with callback startup.
func (conn *Connection) stopAdmissionCleanup() {
	conn.admission.cleanupID++
	if conn.admission.cleanupTimer != nil {
		conn.admission.cleanupTimer.Stop()
		conn.admission.cleanupTimer = nil
	}
}

// scheduleAdmissionCleanup runs often enough to rotate idle streams and enforce
// TTL even when no new admission records arrive.
func (r *Writer) scheduleAdmissionCleanup(connectionName string, conn *Connection) {
	conn.stopAdmissionCleanup()
	cleanupID := conn.admission.cleanupID
	interval := conn.admission.limits.maxFileAge
	if interval > maxAdmissionCleanupInterval {
		interval = maxAdmissionCleanupInterval
	}
	if interval < minAdmissionFileAge {
		interval = minAdmissionFileAge
	}
	conn.admission.cleanupTimer = time.AfterFunc(interval, func() {
		r.runAdmissionCleanup(connectionName, cleanupID)
	})
}

// runAdmissionCleanup exits when its generation is stale, preventing an old
// timer from overwriting newer Connection state.
func (r *Writer) runAdmissionCleanup(connectionName string, cleanupID uint64) {
	connLock, exists := r.acquireCurrentConnectionLock(connectionName, false)
	if !exists {
		return
	}
	defer connLock.Unlock()
	r.mu.Lock()
	conn, exists := r.openConnections[connectionName]
	r.mu.Unlock()
	if !exists || conn.admission.cleanupID != cleanupID {
		return
	}
	conn.stopAdmissionCleanup()
	hasAdmissionArtifacts, err := conn.recoverAdmissionFiles()
	if err != nil {
		log.Error(err, "recovering and cleaning admission export files", "connection", connectionName)
		r.scheduleAdmissionCleanup(connectionName, &conn)
	} else if hasAdmissionArtifacts {
		r.scheduleAdmissionCleanup(connectionName, &conn)
	}
	r.mu.Lock()
	if r.connectionLockIsCurrentLocked(connectionName, connLock) {
		r.openConnections[connectionName] = conn
	}
	r.mu.Unlock()
}

// recoverAdmissionOpenFile publishes only complete JSONL records from an
// abandoned .open file. A file locked by another process is considered live.
func recoverAdmissionOpenFile(path string, maxBytes int64) error {
	file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	fd := int(file.Fd())
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return errAdmissionFileBusy
		}
		return err
	}
	locked := true
	closed := false
	defer func() {
		if locked {
			_ = syscall.Flock(fd, syscall.LOCK_UN)
		}
		if !closed {
			_ = file.Close()
		}
	}()
	unlockAndClose := func() error {
		unlockErr := syscall.Flock(fd, syscall.LOCK_UN)
		locked = false
		closeErr := file.Close()
		if closeErr == nil || errors.Is(closeErr, os.ErrClosed) {
			closed = true
		}
		return errors.Join(unlockErr, closeErr)
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	limit := info.Size()
	if limit > maxBytes {
		limit = maxBytes
		if err := file.Truncate(limit); err != nil {
			return err
		}
	}
	lastNewline, err := findLastNewline(file, limit)
	if err != nil {
		return err
	}
	if lastNewline < 0 {
		if err := unlockAndClose(); err != nil {
			return err
		}
		return os.Remove(path)
	}
	if lastNewline+1 < limit {
		if err := file.Truncate(lastNewline + 1); err != nil {
			return err
		}
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := unlockAndClose(); err != nil {
		return err
	}
	readyPath := strings.TrimSuffix(path, admissionOpenExtension) + ".recovered" + admissionReadyExtension
	return os.Rename(path, readyPath)
}

func findLastNewline(file *os.File, size int64) (int64, error) {
	buffer := make([]byte, 4096)
	for end := size; end > 0; {
		start := end - int64(len(buffer))
		if start < 0 {
			start = 0
		}
		chunk := buffer[:end-start]
		if _, err := file.ReadAt(chunk, start); err != nil && !errors.Is(err, io.EOF) {
			return -1, err
		}
		if index := strings.LastIndexByte(string(chunk), '\n'); index >= 0 {
			return start + int64(index), nil
		}
		end = start
	}
	return -1, nil
}
