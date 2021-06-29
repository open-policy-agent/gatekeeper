package gktest

import "io/fs"

// ReadSuites returns the set of test Suites selected by path.
//
// 1) If path is a path to a Suite, parses and returns the Suite.
// 2) If the path is a directory, returns the Suites defined in that directory
//    (not recursively).
// 3) If the path is a directory followed by "...", returns all Suites in that
//      directory and its subdirectories.
//
// Returns an error if:
// - path is a file that does not define a Suite
// - any matched files containing Suites are not parseable
func ReadSuites(f fs.FS, path string) ([]Suite, error) {
	return nil, nil
}
