package disk

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/util/retry"
)

func (conn *Connection) handleAuditStart(auditID string, topic string) error {
	// Replace ':' with '_' to avoid issues with file names in windows
	conn.currentAuditRun = strings.ReplaceAll(auditID, ":", "_")

	dir := path.Join(conn.Path, topic)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Set the dir permissions to make sure reader can modify files if need be after the lock is released.
	if err := os.Chmod(dir, 0o777); err != nil {
		return fmt.Errorf("failed to set directory permissions: %w", err)
	}

	file, err := os.OpenFile(path.Join(dir, appendExtension(conn.currentAuditRun, "txt")), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	conn.File = file
	err = retry.OnError(retry.DefaultBackoff, func(_ error) bool {
		return true
	}, func() error {
		return syscall.Flock(int(conn.File.Fd()), syscall.LOCK_EX)
	})
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	log.Info("Writing latest violations in", "filename", conn.File.Name())
	return nil
}

func (conn *Connection) handleAuditEnd(topic string) error {
	if err := retry.OnError(retry.DefaultBackoff, func(_ error) bool {
		return true
	}, conn.unlockAndCloseFile); err != nil {
		return fmt.Errorf("error closing file: %w, %s", err, conn.currentAuditRun)
	}
	conn.File = nil

	readyFilePath := path.Join(conn.Path, topic, appendExtension(conn.currentAuditRun, "log"))
	if err := os.Rename(path.Join(conn.Path, topic, appendExtension(conn.currentAuditRun, "txt")), readyFilePath); err != nil {
		return fmt.Errorf("failed to rename file: %w, %s", err, conn.currentAuditRun)
	}
	// Set the file permissions to make sure reader can modify files if need be after the lock is released.
	if err := os.Chmod(readyFilePath, 0o777); err != nil {
		return fmt.Errorf("failed to set file permissions: %w", err)
	}
	log.Info("File renamed", "filename", readyFilePath)

	return conn.cleanupOldAuditFiles(topic)
}

func (conn *Connection) unlockAndCloseFile() error {
	if conn.File == nil {
		return fmt.Errorf("no file to close")
	}
	fd := int(conn.File.Fd())
	if fd < 0 {
		return fmt.Errorf("invalid file descriptor")
	}
	if err := syscall.Flock(fd, syscall.LOCK_UN); err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}
	if err := conn.File.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}
	return nil
}

func (conn *Connection) cleanupOldAuditFiles(topic string) error {
	dirPath := path.Join(conn.Path, topic)
	files, err := getFilesSortedByModTimeAsc(dirPath)
	if err != nil {
		return fmt.Errorf("failed removing older audit files, error getting files sorted by mod time: %w", err)
	}
	var errs []error
	for i := 0; i < len(files)-conn.MaxAuditResults; i++ {
		if e := os.Remove(files[i]); e != nil {
			errs = append(errs, fmt.Errorf("error removing file: %w", e))
		}
	}

	return errors.Join(errs...)
}

func getFilesSortedByModTimeAsc(dirPath string) ([]string, error) {
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var filesInfo []fileInfo

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			filesInfo = append(filesInfo, fileInfo{path: path, modTime: info.ModTime()})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(filesInfo, func(i, j int) bool {
		return filesInfo[i].modTime.Before(filesInfo[j].modTime)
	})

	var sortedFiles []string
	for _, fi := range filesInfo {
		sortedFiles = append(sortedFiles, fi.path)
	}

	return sortedFiles, nil
}

func appendExtension(name string, ext string) string {
	return name + "." + ext
}
