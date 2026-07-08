package disk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/util/retry"
)

var lockFile = func(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func validatePathSegment(name string, value string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", name)
	}
	if value == "." || value == ".." || strings.ContainsAny(value, `/\`) || filepath.IsAbs(value) {
		return fmt.Errorf("%s must be a single path segment", name)
	}
	return nil
}

func auditDirPath(basePath string, topic string) (string, error) {
	if err := validatePathSegment("topic", topic); err != nil {
		return "", err
	}
	return filepath.Join(basePath, topic), nil
}

func auditRunFilePath(basePath string, topic string, auditRun string, ext string) (string, error) {
	if err := validatePathSegment("audit ID", auditRun); err != nil {
		return "", err
	}
	dir, err := auditDirPath(basePath, topic)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appendExtension(auditRun, ext)), nil
}

func (conn *Connection) handleAuditStart(auditID string, topic string) error {
	// Replace ':' with '_' to avoid issues with file names in windows
	auditRun := strings.ReplaceAll(auditID, ":", "_")
	if err := validatePathSegment("audit ID", auditRun); err != nil {
		return err
	}
	if conn.File != nil {
		return fmt.Errorf("audit file already open for audit run %s", conn.currentAuditRun)
	}

	dir, err := auditDirPath(conn.Path, topic)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Set the dir permissions to make sure reader can modify files if need be after the lock is released.
	if err := os.Chmod(dir, 0o770); err != nil {
		return fmt.Errorf("failed to set directory permissions: %w", err)
	}

	filePath, err := auditRunFilePath(conn.Path, topic, auditRun, "txt")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	err = retry.OnError(retry.DefaultBackoff, func(_ error) bool {
		return true
	}, func() error {
		return lockFile(file)
	})
	if err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return fmt.Errorf("failed to acquire lock: %w", errors.Join(err, closeErr))
		}
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	conn.currentAuditRun = auditRun
	conn.File = file
	log.Info("Writing latest violations in", "filename", conn.File.Name())
	return nil
}

func (conn *Connection) handleAuditEnd(topic string) error {
	readyFilePath, err := auditRunFilePath(conn.Path, topic, conn.currentAuditRun, "log")
	if err != nil {
		return err
	}
	tmpFilePath, err := auditRunFilePath(conn.Path, topic, conn.currentAuditRun, "txt")
	if err != nil {
		return err
	}
	if err := retry.OnError(retry.DefaultBackoff, func(_ error) bool {
		return true
	}, conn.unlockAndCloseFile); err != nil {
		return fmt.Errorf("error closing file: %w, %s", err, conn.currentAuditRun)
	}
	conn.File = nil

	if err := os.Rename(tmpFilePath, readyFilePath); err != nil {
		return fmt.Errorf("failed to rename file: %w, %s", err, conn.currentAuditRun)
	}
	// Set the file permissions to make sure reader can modify files if need be after the lock is released.
	if err := os.Chmod(readyFilePath, 0o666); err != nil {
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
	dirPath, err := auditDirPath(conn.Path, topic)
	if err != nil {
		return err
	}
	files, err := getLogFilesSortedByModTimeAsc(dirPath)
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

func getLogFilesSortedByModTimeAsc(dirPath string) ([]string, error) {
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var filesInfo []fileInfo

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		filesInfo = append(filesInfo, fileInfo{path: filepath.Join(dirPath, entry.Name()), modTime: info.ModTime()})
	}

	sort.Slice(filesInfo, func(i, j int) bool {
		if filesInfo[i].modTime.Equal(filesInfo[j].modTime) {
			return filesInfo[i].path < filesInfo[j].path
		}
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
