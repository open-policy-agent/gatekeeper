package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultRootPath      = "/tmp/violations/topics"
	defaultPollInterval  = 5 * time.Second
	readerDirectoryBatch = 128
	readerMaxRecordBytes = 2 << 20
	admissionFilePrefix  = "admission-"
)

func main() {
	root := os.Getenv("ROOT_PATH")
	if root == "" {
		root = defaultRootPath
	}
	deleteAfterRead, err := strconv.ParseBool(os.Getenv("DELETE_AFTER_READ"))
	if err != nil {
		deleteAfterRead = false
	}
	// The test reader uses delete mode for admission segments and retain mode for
	// audit runs, so this flag also selects the filename class to consume.
	info, err := os.Stat(root)
	if err != nil {
		log.Fatalf("failed to stat path: %v", err)
	}
	if !info.IsDir() {
		log.Fatalf("path is not a directory")
	}

	for {
		path, err := findReadyFile(root, deleteAfterRead)
		if err != nil {
			log.Printf("ready file is not available, retrying: %v", err)
			time.Sleep(defaultPollInterval)
			continue
		}
		if err := processReadyFile(path, deleteAfterRead); err != nil {
			log.Printf("processing %s: %v", path, err)
			time.Sleep(defaultPollInterval)
			continue
		}
		if !deleteAfterRead {
			time.Sleep(90 * time.Second)
		}
	}
}

func findOldestReadyFile(root string) (string, error) {
	return findReadyFile(root, true)
}

func findNewestReadyFile(root string) (string, error) {
	return findReadyFile(root, false)
}

// findReadyFile selects admission-prefixed files oldest-first in admission mode
// and audit files newest-first otherwise. Prefix filtering keeps shared channel
// directories safe for both readers.
func findReadyFile(root string, admissionMode bool) (string, error) {
	directory, err := os.Open(root)
	if err != nil {
		return "", err
	}
	defer directory.Close()
	var oldestPath string
	var oldestTime time.Time
	for {
		entries, readErr := directory.ReadDir(readerDirectoryBatch)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", readErr
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path, modTime, findErr := findReadyFileInDir(filepath.Join(root, entry.Name()), admissionMode)
			if findErr != nil {
				return "", findErr
			}
			if path != "" && preferredReadyFile(path, modTime, oldestPath, oldestTime, admissionMode) {
				oldestPath = path
				oldestTime = modTime
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if oldestPath == "" {
		return "", fmt.Errorf("no completed export files in %s", root)
	}
	return oldestPath, nil
}

func findReadyFileInDir(dir string, admissionMode bool) (string, time.Time, error) {
	directory, err := os.Open(dir)
	if err != nil {
		return "", time.Time{}, err
	}
	defer directory.Close()
	var oldestPath string
	var oldestTime time.Time
	for {
		entries, readErr := directory.ReadDir(readerDirectoryBatch)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", time.Time{}, readErr
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" || strings.Contains(entry.Name(), ".deleting") {
				continue
			}
			isAdmission := strings.HasPrefix(entry.Name(), admissionFilePrefix)
			if isAdmission != admissionMode {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return "", time.Time{}, infoErr
			}
			path := filepath.Join(dir, entry.Name())
			if preferredReadyFile(path, info.ModTime(), oldestPath, oldestTime, admissionMode) {
				oldestPath = path
				oldestTime = info.ModTime()
			}
		}
		if errors.Is(readErr, io.EOF) {
			return oldestPath, oldestTime, nil
		}
	}
}

func preferredReadyFile(path string, modTime time.Time, selectedPath string, selectedTime time.Time, admissionMode bool) bool {
	if selectedPath == "" {
		return true
	}
	if modTime.Equal(selectedTime) {
		if admissionMode {
			return path < selectedPath
		}
		return path > selectedPath
	}
	if admissionMode {
		return modTime.Before(selectedTime)
	}
	return modTime.After(selectedTime)
}

// processReadyFile holds an exclusive lock while streaming so driver retention
// cannot delete a file being consumed.
func processReadyFile(path string, deleteAfterRead bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	fd := int(file.Fd())
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
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

	log.Printf("reading from %s", path)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), readerMaxRecordBytes)
	for scanner.Scan() {
		log.Println(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if deleteAfterRead {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	unlockErr := syscall.Flock(fd, syscall.LOCK_UN)
	locked = false
	closeErr := file.Close()
	closed = closeErr == nil || errors.Is(closeErr, os.ErrClosed)
	return errors.Join(unlockErr, closeErr)
}
