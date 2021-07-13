package gktest

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var (
	// ErrNoFileSystem means a method which expects a filesystem got nil
	// instead. This is likely a bug in the code.
	ErrNoFileSystem = errors.New("no filesystem")
	// ErrNoTarget indicates that the user did not specify a target directory or
	// file.
	ErrNoTarget = errors.New("target not specified")
	// ErrUnsupportedExtension indicates that a user attempted to run tests in
	// a file type which is not supported.
	ErrUnsupportedExtension = errors.New("unsupported extension")
	// ErrInvalidYAML indicates that a .yaml/.yml file was not parseable.
	ErrInvalidYAML = errors.New("invalid yaml")
	// ErrNotADirectory indicates that a user is mistakenly attempting to
	// perform a directory-only action on a file (for example, recursively
	// traversing it).
	ErrNotADirectory = errors.New("not a directory")
)

const (
	// Group is the API Group for Test YAML objects.
	Group = "test.gatekeeper.sh"
	// Kind is the Kind for Suite YAML objects.
	Kind = "Suite"
)

// ReadSuites returns the set of test Suites selected by path.
//
// 1) If path is a path to a Suite, parses and returns the Suite.
// 2) If the path is a directory and recursive is false, returns only the Suites
//    defined in that directory.
// 3) If the path is a directory and recursive is true returns all Suites in that
//      directory and its subdirectories.
//
// Returns an error if:
// - path is a file that does not define a Suite
// - any matched files containing Suites are not parseable.
func ReadSuites(f fs.FS, target string, recursive bool) ([]Suite, error) {
	if f == nil {
		return nil, ErrNoFileSystem
	}
	if target == "" {
		return nil, ErrNoTarget
	}

	stat, err := fs.Stat(f, target)
	if err != nil {
		return nil, err
	}

	files := fileList{}
	switch {
	case !stat.IsDir() && !recursive:
		// target is a file.
		err = files.addFile(target)

	case !stat.IsDir() && recursive:
		// target is a file, but the user specified it should be traversed recursively.
		err = fmt.Errorf("can only recursively traverse directories, %w: %q",
			ErrNotADirectory, target)

	case stat.IsDir() && !recursive:
		// target is a directory, but user did not specify to test subdirectories.
		err = files.addDirectory(f, target)

	case stat.IsDir() && recursive:
		// target is a directory, and the user specified it should be traversed recursively.
		err = fs.WalkDir(f, target, files.walkEntry)
	}
	if err != nil {
		return nil, err
	}

	return readSuites(f, files)
}

// readSuites reads the passed set of files into Suites on the given filesystem.
func readSuites(f fs.FS, files []string) ([]Suite, error) {
	var suites []Suite
	for _, file := range files {
		suite, err := readSuite(f, file)
		if err != nil {
			return nil, err
		}
		if suite != nil {
			suites = append(suites, *suite)
		}
	}
	return suites, nil
}

// fileList is a convenience type for breaking apart and deduplicating code
// related to collecting the set of files which may contain Suites.
type fileList []string

func (l *fileList) addFile(target string) error {
	// target is a file.
	ext := filepath.Ext(target)
	if ext != ".yaml" && ext != ".yml" {
		return fmt.Errorf("%w: %q", ErrUnsupportedExtension, ext)
	}
	*l = append(*l, target)
	return nil
}

func (l *fileList) addDirectory(f fs.FS, target string) error {
	// target is a directory, but user did not specify to test subdirectories.
	entries, err := fs.ReadDir(f, target)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		err = l.walkEntry(filepath.Join(target, entry.Name()), entry, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func (l *fileList) walkEntry(path string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	if isYAMLFile(d) {
		*l = append(*l, path)
	}
	return nil
}

func isYAMLFile(d fs.DirEntry) bool {
	if d.IsDir() {
		return false
	}
	ext := filepath.Ext(d.Name())
	return ext == ".yaml" || ext == ".yml"
}

func readSuite(f fs.FS, path string) (*Suite, error) {
	bytes, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, fmt.Errorf("reading file %q: %w", path, err)
	}

	u := unstructured.Unstructured{
		Object: make(map[string]interface{}),
	}
	err = yaml.Unmarshal(bytes, u.Object)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing yaml file %q: %v", ErrInvalidYAML, path, err)
	}
	gvk := u.GroupVersionKind()
	if gvk.Group != Group || gvk.Kind != Kind {
		// Not a test file; we can safely ignore this.
		return nil, nil
	}

	suite := Suite{}
	err = yaml.Unmarshal(bytes, &suite)
	if err != nil {
		return nil, fmt.Errorf("parsing Test %q into Suite: %w", path, err)
	}
	return &suite, nil
}
