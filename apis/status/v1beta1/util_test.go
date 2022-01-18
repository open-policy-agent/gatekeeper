package v1beta1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

var dashingTestCases = []struct {
	name      string
	extracted []string
	packed    string
}{
	{
		name:      "single string no dash",
		extracted: []string{"cat"},
		packed:    "cat",
	},
	{
		name:      "single string, dash",
		extracted: []string{"cat-dog"},
		packed:    "cat--dog",
	},
	{
		name:      "two strings, no dash",
		extracted: []string{"cat", "dog"},
		packed:    "cat-dog",
	},
	{
		name:      "two strings, left dash",
		extracted: []string{"ca-t", "dog"},
		packed:    "ca--t-dog",
	},
	{
		name:      "two strings, right dash",
		extracted: []string{"cat", "d-og"},
		packed:    "cat-d--og",
	},
	{
		name:      "two strings, both dash",
		extracted: []string{"c-at", "do-g"},
		packed:    "c--at-do--g",
	},
	{
		name:      "three strings, no dash",
		extracted: []string{"cat", "dog", "mouse"},
		packed:    "cat-dog-mouse",
	},
	{
		name:      "three strings, left dash",
		extracted: []string{"c-at", "dog", "mouse"},
		packed:    "c--at-dog-mouse",
	},
	{
		name:      "three strings, middle dash",
		extracted: []string{"cat", "do-g", "mouse"},
		packed:    "cat-do--g-mouse",
	},
	{
		name:      "three strings, right dash",
		extracted: []string{"cat", "dog", "mou-se"},
		packed:    "cat-dog-mou--se",
	},
	{
		name:      "three strings, left+middle dash",
		extracted: []string{"ca-t", "d-og", "mouse"},
		packed:    "ca--t-d--og-mouse",
	},
	{
		name:      "three strings, right+middle dash",
		extracted: []string{"cat", "do-g", "m-ouse"},
		packed:    "cat-do--g-m--ouse",
	},
	{
		name:      "three strings, three dash",
		extracted: []string{"ca-t", "do-g", "m-ouse"},
		packed:    "ca--t-do--g-m--ouse",
	},
	{
		name:      "three strings, three dash, double",
		extracted: []string{"ca--t", "do-g", "m-ouse"},
		packed:    "ca----t-do--g-m--ouse",
	},
	{
		name:      "three strings, three dash, double double",
		extracted: []string{"ca--t", "do-g", "m--ouse"},
		packed:    "ca----t-do--g-m----ouse",
	},
}

func TestDashPacker(t *testing.T) {
	for _, tc := range dashingTestCases {
		t.Run(tc.name, func(t *testing.T) {
			gotPacked, err := dashPacker(tc.extracted...)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.packed, gotPacked); diff != "" {
				t.Fatal("got dashPacker(tc.extracted...) != tc.packed, want equal")
			}
		})
	}
}

func TestDashExtractor(t *testing.T) {
	for _, tc := range dashingTestCases {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.extracted, dashExtractor(tc.packed)); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
