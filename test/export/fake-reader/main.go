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
	}
	if !info.IsDir() {
		log.Fatalf("path is not a directory")
	}

	for {
		// Find the latest created file in dirPath
		latestFile, files, err := getLatestFile(dirPath)
		if err != nil {
			log.Println("Latest file is not found, retring in 5 seconds", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Println("available files", files)
		log.Println("reading from", latestFile)
		file, err := os.OpenFile(latestFile, os.O_RDONLY, 0o644)
		if err != nil {
			log.Println("Error opening file", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Acquire an exclusive lock on the file
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
			log.Fatalln("Error locking file", err)
		}

		// Read the file content
		scanner := bufio.NewScanner(file)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			log.Fatalln("Error reading file", err)
		}

		// Process the read content
		for _, line := range lines {
			log.Println(line)
		}

		// Release the lock
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
			log.Fatalln("Error unlocking file", err)
		}

		// Close the file
		if err := file.Close(); err != nil {
			log.Fatalln("Error closing file", err)
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
