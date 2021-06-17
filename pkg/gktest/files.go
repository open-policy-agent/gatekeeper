package gktest

// ToTestFiles returns the set of test files selected by path.
//
// 1) If path is a path to a YAML, runs the suites in that file.
// 2) If the path is a directory, runs suites in that directory (not recursively).
// 3) If the path is a directory followed by "...", recursively runs suites
//      in that directory and its subdirectories.
func ToTestFiles(path string) ([]string, error) {
	return nil, nil
}
