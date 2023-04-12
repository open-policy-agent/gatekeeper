package common

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetBool(t *testing.T) {
	ip := true
	tests := []struct {
		name  string
		input *bool
		dft   bool
		want  bool
	}{
		{
			name:  "return default when input is nil",
			input: nil,
			dft:   false,
			want:  false,
		},
		{
			name:  "return passed input as expected",
			input: &ip,
			dft:   false,
			want:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ret := GetBool(tc.input, tc.dft)
			if tc.want != ret {
				t.Errorf("got value:  \n%t\n, wanted: \n%t\n\n diff: \n%s", ret, tc.want, cmp.Diff(ret, tc.want))
			}
		})
	}
}

func TestGetString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		dft   string
		want  string
	}{
		{
			name:  "return default when input is empty",
			input: "",
			dft:   "default",
			want:  "default",
		},
		{
			name:  "return passed input as expected",
			input: "input",
			dft:   "default",
			want:  "input",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ret := GetString(tc.input, tc.dft)
			if tc.want != ret {
				t.Errorf("got value:  \n%s\n, wanted: \n%s\n\n diff: \n%s", ret, tc.want, cmp.Diff(ret, tc.want))
			}
		})
	}
}

func TestGetInt(t *testing.T) {
	tests := []struct {
		name  string
		input string
		dft   int
		want  int
	}{
		{
			name:  "return default when input is empty",
			input: "",
			dft:   10,
			want:  10,
		},
		{
			name:  "return passed input as expected",
			input: "10",
			dft:   5,
			want:  10,
		},
		{
			name:  "return passed default when input cannot be parsed to int",
			input: "10x",
			dft:   5,
			want:  5,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ret := GetInt(tc.input, tc.dft)
			if tc.want != ret {
				t.Errorf("got value:  \n%v\n, wanted: \n%v\n\n diff: \n%s", ret, tc.want, cmp.Diff(ret, tc.want))
			}
		})
	}
}
