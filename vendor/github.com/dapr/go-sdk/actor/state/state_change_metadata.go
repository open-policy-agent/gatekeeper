/*
Copyright 2021 The Dapr Authors
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

package state

import "time"

type ChangeKind string

const (
	None   = ChangeKind("")
	Add    = ChangeKind("upsert")
	Update = ChangeKind("upsert")
	Remove = ChangeKind("delete")
)

type ChangeMetadata struct {
	Kind  ChangeKind
	Value any
	TTL   *time.Duration
}

func NewChangeMetadata(kind ChangeKind, value any) *ChangeMetadata {
	return &ChangeMetadata{
		Kind:  kind,
		Value: value,
	}
}

func (c *ChangeMetadata) WithTTL(ttl time.Duration) *ChangeMetadata {
	c.TTL = &ttl
	return c
}
