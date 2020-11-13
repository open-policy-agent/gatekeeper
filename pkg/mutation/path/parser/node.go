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
}

// Path represents an entire parsed path specification
type Path struct {
	Nodes []Node
}

func (r Path) Type() NodeType {
	return PathNode
}

type Object struct {
	Reference string
}

func (o Object) Type() NodeType {
	return ObjectNode
}

type List struct {
	KeyField string
	KeyValue *string
	Glob     bool
}

func (l List) Type() NodeType {
	return ListNode
}

func (l List) Value() (string, bool) {
	if l.KeyValue == nil {
		return "", false
	}
	return *l.KeyValue, true
}
