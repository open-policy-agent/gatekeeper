package reader

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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
	if want := "# literal content\n\n"; got != want {
		t.Fatalf("block scalar = %q, want %q", got, want)
	}
}
