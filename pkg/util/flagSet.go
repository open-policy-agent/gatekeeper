package util

import (
	"flag"
	"fmt"
)

type FlagSet map[string]bool

var _ flag.Value = FlagSet{}

func NewFlagSet() FlagSet {
	return make(map[string]bool)
}

func (l FlagSet) ToSlice() []string {
	contents := make([]string, 0)
	for k := range l {
		contents = append(contents, k)
	}
	return contents
}

func (l FlagSet) String() string {
	return fmt.Sprintf("%s", l.ToSlice())
}

func (l FlagSet) Set(s string) error {
	l[s] = true
	return nil
}
