package templates

// While multiple `init()` functions might seem like the way to handle this, the dependency between
// the two functions makes this a poor solution.  Golang orders the files in a package
// lexicographically and then runs their init() functions in that order.  As crd_scheme.go comes
// before scheme.go, initializeCTSchemaMap() was run before initializeSchema().  This caused a
// null-pointer exception, as the Scheme used throughout the package hadn't yet been initialized.
// Calling them in order here fixes that problem.
func init() {
	initializeScheme()
	initializeCTSchemaMap()
}
