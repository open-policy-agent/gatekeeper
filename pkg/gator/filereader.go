package gator

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var allowedExtensions = []string{".yaml", ".yml", ".json"}

func ReadSources(filenames []string) ([]*unstructured.Unstructured, error) {
	var unstrucs []*unstructured.Unstructured

	// read from flags if available
	us, err := ReadFiles(filenames)
	if err != nil {
		return nil, fmt.Errorf("reading from filenames: %w", err)
	}
	unstrucs = append(unstrucs, us...)

	// check if stdin has data.  Read if so.
	us, err = readStdin()
	if err != nil {
		return nil, fmt.Errorf("reading from stdin: %w", err)
	}
	unstrucs = append(unstrucs, us...)

	return unstrucs, nil
}

func ReadFiles(filenames []string) ([]*unstructured.Unstructured, error) {
	var unstrucs []*unstructured.Unstructured

	// verify that the filenames aren't themselves disallowed extensions.  This
	// yields a much better user experience when the user mis-uses the
	// --filename flag.
	for _, name := range filenames {
		// make sure it's a file, not a directory
		fileInfo, err := os.Stat(name)
		if err != nil {
			return nil, fmt.Errorf("stat on path %q: %w", name, err)
		}

		if fileInfo.IsDir() {
			continue
		}
		if !allowedExtension(name) {
			return nil, fmt.Errorf("path %q must be of extensions: %v", name, allowedExtensions)
		}
	}

	// normalize directories by listing their files
	normalized, err := normalize(filenames)
	if err != nil {
		return nil, fmt.Errorf("normalizing filenames: %w", err)
	}

	for _, filename := range normalized {
		file, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("opening file %q: %w", filename, err)
		}
		defer file.Close()

		us, err := ReadK8sResources(bufio.NewReader(file))
		if err != nil {
			return nil, fmt.Errorf("reading file %q: %w", filename, err)
		}

		unstrucs = append(unstrucs, us...)
	}

	return unstrucs, nil
}

func readStdin() ([]*unstructured.Unstructured, error) {
	stdinfo, err := os.Stdin.Stat()
	if err != nil {
		return nil, fmt.Errorf("getting stdin info: %w", err)
	}

	if stdinfo.Size() == 0 {
		return nil, nil
	}

	us, err := ReadK8sResources(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("reading: %w", err)
	}

	return us, nil
}

func normalize(filenames []string) ([]string, error) {
	var output []string

	for _, filename := range filenames {
		paths, err := filesBelow(filename)
		if err != nil {
			return nil, fmt.Errorf("filename %q: %w", filename, err)
		}
		output = append(output, paths...)
	}

	return output, nil
}

// filesBelow walks the filetree from startPath and below, collecting a list of
// all the filepaths.  Directories are excluded.
func filesBelow(startPath string) ([]string, error) {
	var files []string

	err := filepath.Walk(startPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// only add files to the normalized output
		if info.IsDir() {
			return nil
		}

		// make sure the file extension is valid
		if !allowedExtension(path) {
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking: %w", err)
	}

	return files, nil
}

func allowedExtension(path string) bool {
	for _, ext := range allowedExtensions {
		if ext == filepath.Ext(path) {
			return true
		}
	}

	return false
}
