package reader

import (
	"fmt"
	"strings"
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

func TestReadUnstructuredsSeparatorWhitespace(t *testing.T) {
	const first = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: first\ndata:\n  script: |+\n    first\n\n    # literal content\n    last\n\n"
	const second = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: second\ndata:\n  script: |-\n    tail"
	for _, separator := range []string{"---", "--- ", "---\t", "--- \t", "--- # comment", "---\t# comment", "--- \t # comment"} {
		for _, newline := range []string{"\n", "\r\n"} {
			t.Run(fmt.Sprintf("separator_%q_newline_%q", separator, newline), func(t *testing.T) {
				inputs := []string{
					first + separator + "\n" + second,
					"# leading empty document\n" + separator + "\n" + first + separator +
						"\n# middle empty document\n" + separator + "\n" + second + "\n" + separator,
				}
				for _, input := range inputs {
					objects, err := ReadUnstructureds([]byte(strings.ReplaceAll(input, "\n", newline)))
					if err != nil {
						t.Fatal(err)
					}
					if len(objects) != 2 {
						t.Fatalf("got %d objects, want 2", len(objects))
					}
					for i, want := range []string{"first\n\n# literal content\nlast\n\n", "tail"} {
						got, found, err := unstructured.NestedString(objects[i].Object, "data", "script")
						if err != nil || !found {
							t.Fatalf("reading script %d: found=%v, err=%v", i, found, err)
						}
						if got != want {
							t.Fatalf("block scalar %d = %q, want %q", i, got, want)
						}
					}
				}
			})
		}
	}
}

func TestReadUnstructuredsRejectsTabIndentation(t *testing.T) {
	const invalid = "\tapiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: invalid\n"
	for _, input := range []string{invalid, "# empty document\n---\t# separator\n" + invalid} {
		if _, err := ReadUnstructureds([]byte(input)); err == nil {
			t.Fatal("expected invalid tab indentation to be rejected")
		}
	}
}

func TestReadUnstructuredsRejectsInvalidSeparatorCharacters(t *testing.T) {
	for _, suffix := range []string{"\v", "\f", "\x00", "\u00a0", "\t# comment\v", "\t# comment\f"} {
		t.Run(fmt.Sprintf("suffix_%q", suffix), func(t *testing.T) {
			input := "# empty document\n---" + suffix + "\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: invalid\n"
			if _, err := ReadUnstructureds([]byte(input)); err == nil {
				t.Fatal("expected invalid separator characters to be rejected")
			}
		})
	}
}
