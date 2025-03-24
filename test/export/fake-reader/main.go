package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	dirPath := "/tmp/violations/"
	info, err := os.Stat(dirPath)
	if err != nil {
		log.Fatalf("failed to stat path: %v", err)
		os.Exit(1)
	}
	if !info.IsDir() {
		log.Fatalf("path is not a directory")
		os.Exit(1)
	}

	for {
		// Find the latest created file in dirPath
		latestFile, files, err := getLatestFile(dirPath)
		log.Printf("out of all files: %v, reading from just %s \n", files, latestFile)
		if err != nil {
			log.Printf("Error finding latest file: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}
		file, err := os.OpenFile(latestFile, os.O_RDONLY, 0o644)
		if err != nil {
			log.Printf("Error opening file: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Acquire an exclusive lock on the file
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
			log.Fatalf("Error locking file: %v\n", err)
		}

		// Read the file content
		scanner := bufio.NewScanner(file)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			log.Fatalf("Error reading file: %v\n", err)
		}

		// Process the read content
		for _, line := range lines {
			log.Printf("%s\n", line)
		}

		// Release the lock
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
			log.Fatalf("Error unlocking file: %v\n", err)
		}

		// Close the file
		if err := file.Close(); err != nil {
			log.Fatalf("Error closing file: %v\n", err)
		}
		time.Sleep(90 * time.Second)
	}
}

func getLatestFile(dirPath string) (string, []string, error) {
	var latestFile string
	var latestModTime time.Time
	var files []string

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.Contains(path, ".log") && (latestFile == "" || info.ModTime().After(latestModTime)) {
			latestFile = path
			latestModTime = info.ModTime()
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", files, err
	}

	if latestFile == "" {
		return "", files, fmt.Errorf("no files found in directory: %s", dirPath)
	}

	return latestFile, files, nil
}
