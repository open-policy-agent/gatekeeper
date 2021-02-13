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

type NodeType string

const (
	PathNode   NodeType = "Path"
	ListNode   NodeType = "List"
	ObjectNode NodeType = "Object"
)

type Node interface {
	Type() NodeType
	DeepCopyNode() Node
}

// Path represents an entire parsed path specification
type Path struct {
	Nodes []Node
}

var _ Node = Path{}

func (r Path) Type() NodeType {
	return PathNode
}

func (r Path) DeepCopyNode() Node {
	rout := r.DeepCopy()
	return &rout
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

type Object struct {
	Reference string
}

var _ Node = Object{}

func (o Object) Type() NodeType {
	return ObjectNode
}

func (o Object) DeepCopyNode() Node {
	oout := o.DeepCopy()
	return &oout
}

func (o Object) DeepCopy() Object {
	return Object{
		Reference: o.Reference,
	}
}

type List struct {
	KeyField string
	KeyValue *string
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
	if l.KeyValue != nil {
		out.KeyValue = new(string)
		*out.KeyValue = *l.KeyValue
	}
	return out
}

func (l List) Value() (string, bool) {
	if l.KeyValue == nil {
		return "", false
	}
	return *l.KeyValue, true
}
