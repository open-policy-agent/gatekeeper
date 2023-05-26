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

package actor

import (
	"sync"
)

// Client is the interface that should be impl by user's actor client.
type Client interface {
	// Type defines the type of the actor server to be invoke
	Type() string
	// ID should be unique, the actor server with target ID would be created before server processing the invocation.
	ID() string
}

// Server is the interface that would be impl by user's actor server with ServerImplBase
/*
Actor user should only impls func Type() string, and his user-defined-method, Other function could be impl by
combining  ServerImplBase.
*/
type Server interface {
	// ID is impl by ServerImplBase. It can be called by user defined actor function to get the actor ID of it's instance.
	ID() string
	// SetID is impl by ServerImplBase. It is called by actor container to inject actor ID of the instance, and should
	// not called by user
	SetID(string)
	// Type is defined by user
	Type() string
	// SetStateManager is impl by ServerImplBase to inject StateManager to this actor instance
	SetStateManager(StateManager)
	// SaveState is impl by ServerImplBase, It saves the state cache of this actor instance to state store component by calling api of daprd.
	// Save state is called at two places: 1. On invocation of this actor instance. 2. When new actor starts.
	SaveState() error
}

type ReminderCallee interface {
	ReminderCall(string, []byte, string, string)
}

type Factory func() Server

type ServerImplBase struct {
	stateManager StateManager
	once         sync.Once
	id           string
}

func (b *ServerImplBase) SetStateManager(stateManager StateManager) {
	b.stateManager = stateManager
}

// GetStateManager can be called by user-defined-method, to get state manager of this actor instance.
func (b *ServerImplBase) GetStateManager() StateManager {
	return b.stateManager
}

func (b *ServerImplBase) ID() string {
	return b.id
}

func (b *ServerImplBase) SetID(id string) {
	b.once.Do(func() {
		b.id = id
	})
}

// SaveState is to saves the state cache of this actor instance to state store component by calling api of daprd.
func (b *ServerImplBase) SaveState() error {
	if b.stateManager != nil {
		return b.stateManager.Save()
	}
	return nil
}

type StateManager interface {
	// Add is to add new state store with @stateName and @value
	Add(stateName string, value interface{}) error
	// Get is to get state store of @stateName with type @reply
	Get(stateName string, reply interface{}) error
	// Set is to set new state store with @stateName and @value
	Set(stateName string, value interface{}) error
	// Remove is to remove state store with @stateName
	Remove(stateName string) error
	// Contains is to check if state store contains @stateName
	Contains(stateName string) (bool, error)
	// Save is to saves the state cache of this actor instance to state store component by calling api of daprd.
	Save() error
	// Flush is called by stateManager after Save
	Flush()
}
