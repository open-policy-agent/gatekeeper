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

type PubsubMsg struct {
	ID                    string            `json:"id,omitempty"`
	Details               interface{}       `json:"details,omitempty"`
	EventType             string            `json:"eventType,omitempty"`
	Group                 string            `json:"group,omitempty"`
	Version               string            `json:"version,omitempty"`
	Kind                  string            `json:"kind,omitempty"`
	Name                  string            `json:"name,omitempty"`
	Namespace             string            `json:"namespace,omitempty"`
	Message               string            `json:"message,omitempty"`
	EnforcementAction     string            `json:"enforcementAction,omitempty"`
	ConstraintAnnotations map[string]string `json:"constraintAnnotations,omitempty"`
	ResourceGroup         string            `json:"resourceGroup,omitempty"`
	ResourceAPIVersion    string            `json:"resourceAPIVersion,omitempty"`
	ResourceKind          string            `json:"resourceKind,omitempty"`
	ResourceNamespace     string            `json:"resourceNamespace,omitempty"`
	ResourceName          string            `json:"resourceName,omitempty"`
	ResourceLabels        map[string]string `json:"resourceLabels,omitempty"`
}

// Modifications for acturate simulation
// varify if violation exists for a constraint owned by policy and then post it - add sleep (2s) for a batch size of 2k violations
// hold 2k violations in variable - read from tmp-violations.txt
// hold tmp file for previous violations
// 2 files
// 1 - GK publish violations
// 1 - policy read violations.
func main() {
	dirPath := "/tmp/violations/"

	for {
		// Find the latest created file in dirPath
		latestFile, files, err := getLatestFile(dirPath)
		log.Printf("out of all files: %v, reading from just %s \n", files, latestFile)
		if err != nil {
			log.Printf("Error finding latest file: %v\n", err)
			time.Sleep(5 * time.Second)
			continue
		}
		// Open the file in read-write mode
		file, err := os.OpenFile(latestFile, os.O_RDWR, 0o644)
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
