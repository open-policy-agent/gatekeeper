package path

// Parse parses the string representing a path specification
// and returns the path splitted as path entries
func Parse(string) ([]Entry, error) {
	return nil, nil
}

//Type is the type of a path
type Type string

// Entry represents a segment of a path
type Entry interface {
	Type() Type
}

// Object is a segment of a path which is not a list
type Object struct {
	// TypeName is the type of Entry
	Type Type
	// The name of the field corresponding to the next segment of the path.
	PointsTo string
}

// List is a segment of a path that relates to a list
type List struct {
	Type     Type
	KeyField string
	KeyValue *string
	Globbed  bool
}
