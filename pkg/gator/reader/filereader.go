package reader

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/oci"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var allowedExtensions = []string{gator.ExtYAML, gator.ExtYML, gator.ExtJSON}

func ReadSources(filenames []string, images []string, tempDir string) ([]*unstructured.Unstructured, error) {
	var sources []*source

	// Read from --filename flag
	s, err := readFiles(filenames)
	if err != nil {
		return nil, fmt.Errorf("reading from filenames: %w", err)
	}
	sources = append(sources, s...)

	// Read from --image flag
	s, err = readImages(images, tempDir)
	if err != nil {
		return nil, fmt.Errorf("pulling image: %w", err)
	}
	sources = append(sources, s...)

	// Read stdin
	stdinUnstructs, err := readStdin()
	if err != nil {
		return nil, fmt.Errorf("reading from stdin: %w", err)
	}
	sources = append(sources, &source{stdin: true, objs: stdinUnstructs})

	conflicts := detectConflicts(sources)
	for i := range conflicts {
		logConflict(&conflicts[i])
	}

	return sourcesToUnstruct(sources), nil
}

func readImage(image string, tempDir string) ([]*source, error) {
	dirPath, closeHandler, err := oci.PullImage(image, tempDir)
	if closeHandler != nil {
		defer closeHandler()
	}
	if err != nil {
		return nil, err
	}

	sources, err := readFile(dirPath)
	if err != nil {
		return nil, err
	}
	for _, s := range sources {
		s.image = image
	}

	return sources, nil
}

func readImages(images []string, tempDir string) ([]*source, error) {
	var sources []*source
	for _, image := range images {
		s, err := readImage(image, tempDir)
		if err != nil {
			return nil, err
		}
		sources = append(sources, s...)
	}

	return sources, nil
}

func readFile(filename string) ([]*source, error) {
	if err := verifyFile(filename); err != nil {
		return nil, err
	}

	var sources []*source
	expanded, err := expandDirectories([]string{filename})
	if err != nil {
		return nil, fmt.Errorf("normalizing filenames: %w", err)
	}

	for _, f := range expanded {
		file, err := os.Open(f)
		if err != nil {
			return nil, fmt.Errorf("opening file %q: %w", f, err)
		}
		defer file.Close()

		us, err := ReadK8sResources(bufio.NewReader(file))
		if err != nil {
			return nil, fmt.Errorf("reading file %q: %w", f, err)
		}

		sources = append(sources, &source{
			filename: f,
			objs:     us,
		})
	}

	return sources, nil
}

func readFiles(filenames []string) ([]*source, error) {
	var sources []*source
	for _, f := range filenames {
		s, err := readFile(f)
		if err != nil {
			return nil, err
		}
		sources = append(sources, s...)
	}

	return sources, nil
}

// verifyFile checks that the filenames aren't themselves disallowed extensions.
// This yields a much better user experience when the user mis-uses the
// --filename flag.
func verifyFile(filename string) error {
	// make sure it's a file, not a directory
	fileInfo, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("stat on path %q: %w", filename, err)
	}

	if fileInfo.IsDir() {
		return nil
	}
	if !allowedExtension(filename) {
		return fmt.Errorf("path %q must be of extensions: %v", filename, allowedExtensions)
	}

	return nil
}

func readStdin() ([]*unstructured.Unstructured, error) {
	stdinfo, err := os.Stdin.Stat()
	if err != nil {
		return nil, fmt.Errorf("getting stdin info: %w", err)
	}

	// check if data is being piped or redirected to stdin
	if (stdinfo.Mode() & os.ModeCharDevice) != 0 {
		return nil, nil
	}

	us, err := ReadK8sResources(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("reading: %w", err)
	}

	return us, nil
}

func expandDirectories(filenames []string) ([]string, error) {
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

func sourcesToUnstruct(sources []*source) []*unstructured.Unstructured {
	var us []*unstructured.Unstructured
	for _, s := range sources {
		us = append(us, s.objs...)
	}
	return us
}
