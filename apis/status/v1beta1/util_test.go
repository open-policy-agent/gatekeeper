package v1beta1

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestOwnership(t *testing.T) {
	g := NewGomegaWithT(t)
	t.Run("Ownership defaults to enabled", func(t *testing.T) {
		g.Expect(PodOwnershipEnabled()).Should(BeTrue())
	})
	t.Run("Disabling is honored", func(t *testing.T) {
		DisablePodOwnership()
		g.Expect(PodOwnershipEnabled()).Should(BeFalse())
	})
}

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
	g := NewGomegaWithT(t)
	for _, tc := range dashingTestCases {
		t.Run(tc.name, func(t *testing.T) {
			g.Expect(dashPacker(tc.extracted...)).To(Equal(tc.packed))
		})
	}
}

func TestDashExtractor(t *testing.T) {
	g := NewGomegaWithT(t)
	for _, tc := range dashingTestCases {
		t.Run(tc.name, func(t *testing.T) {
			g.Expect(dashExtractor(tc.packed)).To(Equal(tc.extracted))
		})
	}
}
