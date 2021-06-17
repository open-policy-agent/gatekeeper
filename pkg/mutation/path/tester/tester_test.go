package tester

import (
	"errors"
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
			var wantErr error
			if !test.prefix {
				wantErr = ErrPrefix
			}
			err = validatePathTests(l, pts)
			if !errors.Is(err, wantErr) {
				t.Errorf(`got validatePathTests() error = '%v', want '%v'`, err, wantErr)
			}
		})
	}
}

func mustParse(p string) parser.Path {
	pth, err := parser.Parse(p)
	if err != nil {
		panic(err)
	}
	return pth
}

func TestConflictingEntries(t *testing.T) {
	tests := []struct {
		name     string
		location string
		ts       []Test
		wantErr  error
	}{
		{
			name:     "contradicting Conditions on same path",
			location: "spec.some.thing",
			ts: []Test{
				{
					SubPath:   mustParse("spec.some.thing"),
					Condition: MustExist,
				},
				{
					SubPath:   mustParse("spec.some.thing"),
					Condition: MustNotExist,
				},
			},
			wantErr: ErrConflict,
		},
		{
			name:     "same path MustExist twice",
			location: "spec.some.thing",
			ts: []Test{
				{
					SubPath:   mustParse("spec.some.thing"),
					Condition: MustExist,
				},
				{
					SubPath:   mustParse("spec.some.thing"),
					Condition: MustExist,
				},
			},
			wantErr: nil,
		},
		{
			name:     "same path MustNotExist twice",
			location: "spec.some.thing",
			ts: []Test{
				{
					SubPath:   mustParse("spec.some.thing"),
					Condition: MustNotExist,
				},
				{
					SubPath:   mustParse("spec.some.thing"),
					Condition: MustNotExist,
				},
			},
			wantErr: nil,
		},
		{
			name:     "parent required but child forbidden",
			location: "spec.some.thing",
			ts: []Test{
				{
					SubPath:   mustParse("spec.some"),
					Condition: MustExist,
				},
				{
					SubPath:   mustParse("spec.some.thing"),
					Condition: MustNotExist,
				},
			},
			wantErr: nil,
		},
		{
			name:     "parent forbidden but child required",
			location: "spec.some.thing",
			ts: []Test{
				{
					SubPath:   mustParse("spec.some"),
					Condition: MustNotExist,
				},
				{
					SubPath:   mustParse("spec.some.thing"),
					Condition: MustExist,
				},
			},
			wantErr: ErrConflict,
		},
		{
			name:     "grandparent forbidden but grandchild required",
			location: "spec.some.thing.more",
			ts: []Test{
				{
					SubPath:   mustParse("spec.some"),
					Condition: MustNotExist,
				},
				{
					SubPath:   mustParse("spec.some.thing.more"),
					Condition: MustExist,
				},
			},
			wantErr: ErrConflict,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			location, err := parser.Parse(test.location)
			if err != nil {
				t.Fatal(err)
			}

			_, err = New(location, test.ts)
			if !errors.Is(err, test.wantErr) {
				t.Errorf(`got New() error = '%v', want '%v'`, err, test.wantErr)
			}
		})
	}
}

func TestExistsOkay(t *testing.T) {
	tester, err := New(mustParse("spec.location.thing.and.another"),
		[]Test{
			{
				SubPath:   mustParse("spec.location.thing"),
				Condition: MustExist,
			},
			{
				SubPath:   mustParse("spec.location.thing.and.another"),
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
	tester, err := New(mustParse("spec.location.thing.and.another"),
		[]Test{
			{
				SubPath:   mustParse("spec.location.thing"),
				Condition: MustExist,
			},
			{
				SubPath:   mustParse("spec.location.thing.and.another"),
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
