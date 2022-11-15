package reader

import (
	"fmt"
	"log"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const warningMsg = "WARNING - Resource named %q (from %s) is already defined in %s"

type source struct {
	filename string
	image    string
	stdin    bool
	objs     []*unstructured.Unstructured
}

type gknn struct {
	schema.GroupKind
	name      string
	namespace string
}

type conflict struct {
	id gknn
	a  *source
	b  *source
}

func detectConflicts(sources []*source) []conflict {
	var conflicts []conflict
	cmap := make(map[gknn]*source)

	for _, s := range sources {
		for _, obj := range s.objs {
			key := gknn{
				GroupKind: schema.GroupKind{Group: obj.GroupVersionKind().Group, Kind: obj.GetKind()},
				name:      obj.GetName(),
				namespace: obj.GetNamespace(),
			}
			if dupe, exists := cmap[key]; exists {
				conflicts = append(conflicts, conflict{
					id: key,
					a:  s,
					b:  dupe,
				})
			}
			cmap[key] = s
		}
	}

	return conflicts
}

func logConflict(c *conflict) {
	log.Printf(warningMsg+"\n", c.id.name, sourceDebugInfo(c.a), sourceDebugInfo(c.b))
}

// sourceDebugInfo returns a string identifying the source.
// For sources pulled from stdin: "stdin".
// For sources pulled from a file: "file: <filename>".
// For sources pulled from an image: "file: <filename>, image: <imgURL>".
func sourceDebugInfo(s *source) string {
	if s.stdin {
		return "stdin"
	}
	if s.image != "" {
		return fmt.Sprintf("file: %q, image: %q", s.filename, s.image)
	}
	return fmt.Sprintf("file: %q", s.filename)
}
