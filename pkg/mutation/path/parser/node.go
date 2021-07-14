/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package parser

import (
	"fmt"
	"strings"
)

type NodeType string

const (
	// ListNode is an array element of a path.
	ListNode NodeType = "List"
	// ObjectNode is the final Node in a path, what is being referenced.
	ObjectNode NodeType = "Object"
)

type Node interface {
	Type() NodeType
	DeepCopyNode() Node
	// String converts the Node into an equivalent String representation.
	// Calling Parse on the result yields an equivalent Node, but may differ in
	// structure if the Node is a Path containing Path Nodes.
	String() string
}

// Path represents an entire parsed path specification.
type Path struct {
	Nodes []Node
}

func (r Path) DeepCopy() Path {
	out := Path{
		Nodes: make([]Node, len(r.Nodes)),
	}
	for i := 0; i < len(r.Nodes); i++ {
		out.Nodes[i] = r.Nodes[i].DeepCopyNode()
	}
	return out
}

func (r Path) String() string {
	result := strings.Builder{}
	for i, n := range r.Nodes {
		nStr := n.String()
		if n.Type() == ObjectNode && i > 0 {
			// No leading separator, and no separators before List Nodes.
			result.WriteString(".")
		}
		result.WriteString(nStr)
	}
	return result.String()
}

type Object struct {
	Reference string
}

var _ Node = Object{}

func (o Object) Type() NodeType {
	return ObjectNode
}

func (o Object) DeepCopyNode() Node {
	oOut := o.DeepCopy()
	return &oOut
}

func (o Object) DeepCopy() Object {
	return Object{
		Reference: o.Reference,
	}
}

func (o Object) String() string {
	return quote(o.Reference)
}

type List struct {
	KeyField string
	KeyValue interface{}
	Glob     bool
}

var _ Node = List{}

func (l List) Type() NodeType {
	return ListNode
}

func (l List) DeepCopyNode() Node {
	lout := l.DeepCopy()
	return &lout
}

func (l List) DeepCopy() List {
	out := List{}
	out.KeyField = l.KeyField
	out.Glob = l.Glob
	// KeyValue (interface{}) will be one of: [string, int, nil]
	out.KeyValue = l.KeyValue
	return out
}

func (l List) String() string {
	key := quote(l.KeyField)
	if l.Glob {
		return fmt.Sprintf("[%s: *]", key)
	}
	switch v := l.KeyValue.(type) {
	case string:
		q := quote(v)
		return fmt.Sprintf("[%s: %s]", key, q)

	case int, int64:
		return fmt.Sprintf("[%s: %d]", key, v)

	case nil:
	default:
	}
	// Represents an improperly specified List node.
	return fmt.Sprintf("[%s: ]", key)
}

// quote optionally adds double quotes around the passed string if needed.
// Quotes are needed for:
//  * Strings containing whitespace, quotes, or other "ambiguous" characters that will
//    be tokenized as non-strings and need escaping.
//  * Strings starting digits, that would otherwise be tokenized as an integer
//  * Empty strings
func quote(s string) string {
	if len(s) == 0 {
		return `""`
	}
	switch {
	case strings.ContainsAny(s, "'\"\t\n \\*[]:."),
		strings.ContainsAny(s[0:1], "0123456789"):
		// Using fmt.Sprintf with %q converts whitespace to escape sequences, and we
		// don't want that.
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		return `"` + s + `"`
	}

	return s
}
