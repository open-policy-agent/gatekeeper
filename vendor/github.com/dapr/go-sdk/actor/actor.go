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
	"context"
	"sync"
	"time"
)

// Client is the interface that should be impl by user's actor client.
type Client interface {
	// Type defines the type of the actor server to be invoke
	Type() string
	// ID should be unique, the actor server with target ID would be created before server processing the invocation.
	ID() string
}

// Deprecated: Server is deprecated in favour of ServerContext.
type Server interface {
	// ID is impl by ServerImplBase. It can be called by user defined actor function to get the actor ID of it's instance.
	ID() string
	// SetID is impl by ServerImplBase. It is called by actor container to inject actor ID of the instance, and should
	// not called by user
	SetID(string)
	// Type is defined by user
	Type() string
	// SetStateManager is impl by ServerImplBase to inject StateManager to this actor instance
	// Deprecated: SetStateManager is deprecated in favour of SetStateManagerContext.
	SetStateManager(StateManager)
	// SaveState is impl by ServerImplBase, It saves the state cache of this actor instance to state store component by calling api of daprd.
	// Save state is called at two places: 1. On invocation of this actor instance. 2. When new actor starts.
	SaveState() error

	WithContext() ServerContext
}

// ServerContext is the interface that would be impl by user's actor server with ServerImplBaseCtx
/*
Actor user should only impls func Type() string, and his user-defined-method, Other function could be impl by
combining  ServerImplBaseCtx.
*/
type ServerContext interface {
	// ID is impl by ServerImplBase. It can be called by user defined actor function to get the actor ID of it's instance.
	ID() string
	// SetID is impl by ServerImplBase. It is called by actor container to inject actor ID of the instance, and should
	// not called by user
	SetID(string)
	// Type is defined by user
	Type() string
	// SetStateManager is impl by ServerImplBase to inject StateManager to this actor instance
	SetStateManager(StateManagerContext)
	// SaveState is impl by ServerImplBase, It saves the state cache of this actor instance to state store component by calling api of daprd.
	// Save state is called at two places: 1. On invocation of this actor instance. 2. When new actor starts.
	SaveState(context.Context) error
}

type ReminderCallee interface {
	ReminderCall(string, []byte, string, string)
}

type (
	Factory        func() Server
	FactoryContext func() ServerContext
)

// Deprecated: ServerImplBase is deprecated in favour of ServerImplBaseCtx.
type ServerImplBase struct {
	stateManager StateManager
	ctx          ServerImplBaseCtx
	lock         sync.RWMutex
}

type ServerImplBaseCtx struct {
	stateManager StateManagerContext
	once         sync.Once
	id           string
	lock         sync.RWMutex
}

// Deprecated: Use ServerImplBaseCtx instead.
func (b *ServerImplBase) SetStateManager(stateManager StateManager) {
	b.lock.Lock()
	b.ctx.lock.Lock()
	defer b.lock.Unlock()
	defer b.ctx.lock.Unlock()
	b.stateManager = stateManager
	b.ctx.stateManager = stateManager.WithContext()
}

// GetStateManager can be called by user-defined-method, to get state manager
// of this actor instance.
// Deprecated: Use ServerImplBaseCtx instead.
func (b *ServerImplBase) GetStateManager() StateManager {
	b.ctx.lock.RLock()
	defer b.ctx.lock.RUnlock()
	return b.stateManager
}

// Deprecated: Use ServerImplBaseCtx instead.
func (b *ServerImplBase) ID() string {
	b.ctx.lock.RLock()
	defer b.ctx.lock.RUnlock()
	return b.ctx.id
}

// Deprecated: Use ServerImplBaseCtx instead.
func (b *ServerImplBase) SetID(id string) {
	b.lock.RLock()
	defer b.lock.RUnlock()
	b.ctx.SetID(id)
}

// SaveState is to saves the state cache of this actor instance to state store
// component by calling api of daprd.
// Deprecated: Use ServerImplBaseCtx instead.
func (b *ServerImplBase) SaveState() error {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.ctx.SaveState(context.Background())
}

// Deprecated: Use ServerImplBaseCtx instead.
func (b *ServerImplBase) WithContext() *ServerImplBaseCtx {
	b.ctx.lock.RLock()
	defer b.ctx.lock.RUnlock()
	return &b.ctx
}

func (b *ServerImplBaseCtx) SetStateManager(stateManager StateManagerContext) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.stateManager = stateManager
}

// GetStateManager can be called by user-defined-method, to get state manager
// of this actor instance.
func (b *ServerImplBaseCtx) GetStateManager() StateManagerContext {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.stateManager
}

func (b *ServerImplBaseCtx) ID() string {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.id
}

func (b *ServerImplBaseCtx) SetID(id string) {
	b.lock.RLock()
	defer b.lock.RUnlock()
	b.once.Do(func() {
		b.id = id
	})
}

// SaveState is to saves the state cache of this actor instance to state store
// component by calling api of daprd.
func (b *ServerImplBaseCtx) SaveState(ctx context.Context) error {
	b.lock.RLock()
	defer b.lock.RUnlock()

	if b.stateManager != nil {
		return b.stateManager.Save(ctx)
	}

	return nil
}

// Deprecated: StateManager is deprecated in favour of StateManagerContext.
type StateManager interface {
	// Add is to add new state store with @stateName and @value
	Add(stateName string, value any) error
	// Get is to get state store of @stateName with type @reply
	Get(stateName string, reply any) error
	// Set is to set new state store with @stateName and @value
	Set(stateName string, value any) error
	// Remove is to remove state store with @stateName
	Remove(stateName string) error
	// Contains is to check if state store contains @stateName
	Contains(stateName string) (bool, error)
	// Save is to saves the state cache of this actor instance to state store component by calling api of daprd.
	Save() error
	// Flush is called by StateManager after Save
	Flush()

	// Returns a new StateManagerContext with the same state as this StateManager
	// but uses context.
	WithContext() StateManagerContext
}

type StateManagerContext interface {
	// Add is to add new state store with @stateName and @value
	Add(ctx context.Context, stateName string, value any) error
	// Get is to get state store of @stateName with type @reply
	Get(ctx context.Context, stateName string, reply any) error
	// Set sets a state store with @stateName and @value.
	Set(ctx context.Context, stateName string, value any) error
	// SetWithTTL sets a state store with @stateName and @value, for the given
	// TTL. After the TTL has passed, the value will no longer be available with
	// `Get`. Always preferred over `Set`.
	// NOTE: SetWithTTL is in feature preview as of v1.11, and only available
	// with the `ActorStateTTL` feature enabled in Dapr.
	SetWithTTL(ctx context.Context, stateName string, value any, ttl time.Duration) error
	// Remove is to remove state store with @stateName
	Remove(ctx context.Context, stateName string) error
	// Contains is to check if state store contains @stateName
	Contains(ctx context.Context, stateName string) (bool, error)
	// Save is to saves the state cache of this actor instance to state store component by calling api of daprd.
	Save(ctx context.Context) error
	// Flush is called by StateManager after Save
	Flush(ctx context.Context)
}
