package reader

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestReadUnstructuredsPreservesDocumentEnd(t *testing.T) {
	for _, tc := range []struct {
		name, scalar, ending, want string
	}{
		{"clip", "|", "\n", "value\n"},
		{"keep", "|+", "\n\n", "value\n\n"},
		{"strip", "|-", "\n\n", "value"},
		{"no final newline", "|+", "", "value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\ndata:\n  script: %s\n    value%s", tc.scalar, tc.ending)
			inputs := []string{input}
			if tc.ending != "" {
				inputs = append(inputs, input+"---\n# trailing document\n")
			}
			for _, input := range inputs {
				objects, err := ReadUnstructureds([]byte(input))
				if err != nil {
					t.Fatal(err)
				}
				if len(objects) != 1 {
					t.Fatalf("got %d objects, want 1", len(objects))
				}
				got, found, err := unstructured.NestedString(objects[0].Object, "data", "script")
				if err != nil || !found {
					t.Fatalf("reading script: found=%v, err=%v", found, err)
				}
				if got != tc.want {
					t.Fatalf("block scalar = %q, want %q", got, tc.want)
				}
			}
		})
	}
}

func TestReadUnstructuredsPreservesBlockScalar(t *testing.T) {
	input := []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\ndata:\n  script: |\n    first\n\n    # literal content\n    last\n")
	objects, err := ReadUnstructureds(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 {
		t.Fatalf("got %d objects, want 1", len(objects))
	}
	got, found, err := unstructured.NestedString(objects[0].Object, "data", "script")
	if err != nil || !found {
		t.Fatalf("reading script: found=%v, err=%v", found, err)
	}
	want := "first\n\n# literal content\nlast\n"
	if got != want {
		t.Fatalf("block scalar = %q, want %q", got, want)
	}
}

func TestReadUnstructuredsSkipsEmptyDocuments(t *testing.T) {
	input := []byte("# comment only\n\n---\n\n# another comment\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\ndata:\n  script: |+\n    # literal content\n\n\n---\n# trailing comment\n")
	objects, err := ReadUnstructureds(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 {
		t.Fatalf("got %d objects, want 1", len(objects))
	}
	got, found, err := unstructured.NestedString(objects[0].Object, "data", "script")
	if err != nil || !found {
		t.Fatalf("reading script: found=%v, err=%v", found, err)
	}
	if want := "# literal content\n\n\n"; got != want {
		t.Fatalf("block scalar = %q, want %q", got, want)
	}
}
