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

package codec

import (
	"fmt"
)

// Codec is serializer interface.
type Codec interface {
	Marshal(interface{}) ([]byte, error)
	Unmarshal([]byte, interface{}) error
}

// Factory is factory of codec.
type Factory func() Codec

// codecFactoryMap stores.
var codecFactoryMap = make(map[string]Factory)

// SetActorCodec set Actor's Codec.
func SetActorCodec(name string, f Factory) {
	codecFactoryMap[name] = f
}

// GetActorCodec gets the target codec instance.
func GetActorCodec(name string) (Codec, error) {
	f, ok := codecFactoryMap[name]
	if !ok {
		return nil, fmt.Errorf("no actor codec implement named %s", name)
	}
	return f(), nil
}
