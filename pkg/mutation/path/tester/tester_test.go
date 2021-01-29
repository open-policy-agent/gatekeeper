package tester

import (
	"fmt"
	"testing"

	"github.com/open-policy-agent/gatekeeper/pkg/mutation/path/parser"
)

func TestPrefix(t *testing.T) {
	tests := []struct {
		short  string
		long   string
		prefix bool
	}{
		{
			short:  "a.b.c",
			long:   "a.b.c.d",
			prefix: true,
		},
		{
			short:  "a.b.c",
			long:   "a.b.c",
			prefix: true,
		},
		{
			short:  "a[name:b].c",
			long:   "a[name:b].c",
			prefix: true,
		},
		{
			short:  "a[name:\"b\"]",
			long:   "a[name:b].c",
			prefix: true,
		},
		{
			short:  "a.b.q",
			long:   "a.b.c.d",
			prefix: false,
		},
		{
			short:  "a[name:b].c",
			long:   "a[name:b]",
			prefix: false,
		},
		{
			short:  "a[name:\"r\"]",
			long:   "a[name:b].c",
			prefix: false,
		},
		{
			short:  "a[otherthing:\"b\"]",
			long:   "a[name:b].c",
			prefix: false,
		},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("TestPrefix #%d", i), func(t *testing.T) {
			s, err := parser.Parse(test.short)
			if err != nil {
				t.Fatalf("Could not parse %s: %s", test.short, err)
			}
			l, err := parser.Parse(test.long)
			if err != nil {
				t.Fatalf("Could not parse %s: %s", test.long, err)
			}
			prefix := isPrefix(s, l)
			if prefix != test.prefix {
				t.Errorf("isPrefix(%s, %s) = %t, expected %t", test.short, test.long, prefix, test.prefix)
			}
			pts := []Test{
				{
					SubPath:   s,
					Condition: MustExist,
				},
			}
			err = ValidatePathTests(l, pts)
			if (err == nil) != test.prefix {
				t.Errorf("we expect that we will validate that all paths are a prefix of the location")
			}
		})
	}
}

func ppath(p string) *parser.Path {
	pth, err := parser.Parse(p)
	if err != nil {
		panic(err)
	}
	return pth
}

func TestConflictingEntries(t *testing.T) {
	tests := []struct {
		ts            []Test
		errorExpected bool
	}{
		{
			ts: []Test{
				{
					SubPath:   ppath("spec.some.thing"),
					Condition: MustExist,
				},
				{
					SubPath:   ppath("spec.some.thing"),
					Condition: MustNotExist,
				},
			},
			errorExpected: true,
		},
		{
			ts: []Test{
				{
					SubPath:   ppath("spec.some.thing"),
					Condition: MustExist,
				},
				{
					SubPath:   ppath("spec.some.thing"),
					Condition: MustExist,
				},
			},
			errorExpected: false,
		},
		{
			ts: []Test{
				{
					SubPath:   ppath("spec.some"),
					Condition: MustExist,
				},
				{
					SubPath:   ppath("spec.some.thing"),
					Condition: MustNotExist,
				},
			},
			errorExpected: false,
		},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("TestPrefix #%d", i), func(t *testing.T) {
			_, err := New(test.ts)
			if (err != nil) != test.errorExpected {
				t.Errorf("Error exists is %v; wanted %v", (err != nil), test.errorExpected)
			}
		})
	}
}

func TestExistsOkay(t *testing.T) {
	tester, err := New(
		[]Test{
			{
				SubPath:   ppath("spec.location.thing"),
				Condition: MustExist,
			},
			{
				SubPath:   ppath("spec.location.thing.and.another"),
				Condition: MustNotExist,
			},
		},
	)
	if err != nil {
		t.Fatalf("could not create tester: %v", err)
	}
	tests := make([]bool, 10)
	for i := 0; i < 10; i++ {
		tests[i] = true
	}
	tests[4] = false
	for i, expected := range tests {
		if tester.ExistsOkay(i) != expected {
			t.Errorf("ExistsOkay(%d) = %t, wanted %t", i, !expected, expected)
		}
	}
}

func TestMissingOkay(t *testing.T) {
	tester, err := New(
		[]Test{
			{
				SubPath:   ppath("spec.location.thing"),
				Condition: MustExist,
			},
			{
				SubPath:   ppath("spec.location.thing.and.another"),
				Condition: MustNotExist,
			},
		},
	)
	if err != nil {
		t.Fatalf("could not create tester: %v", err)
	}
	tests := make([]bool, 10)
	for i := 0; i < 10; i++ {
		tests[i] = true
	}
	tests[2] = false
	for i, expected := range tests {
		if tester.MissingOkay(i) != expected {
			t.Errorf("MissingOkay(%d) = %t, wanted %t", i, !expected, expected)
		}
	}
}
