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

package manager

import (
	"context"
	"log"
	"reflect"

	"github.com/dapr/go-sdk/actor"
	"github.com/dapr/go-sdk/actor/codec"
	actorErr "github.com/dapr/go-sdk/actor/error"
	"github.com/dapr/go-sdk/actor/state"
	dapr "github.com/dapr/go-sdk/client"
)

// Deprecated: use ActorContainerContext instead.
type ActorContainer interface {
	Invoke(methodName string, param []byte) ([]reflect.Value, actorErr.ActorErr)
	//nolint:staticcheck // SA1019 Deprecated: use ActorContainerContext instead.
	GetActor() actor.Server
}

type ActorContainerContext interface {
	Invoke(ctx context.Context, methodName string, param []byte) ([]reflect.Value, actorErr.ActorErr)
	GetActor() actor.ServerContext
}

// DefaultActorContainer contains actor instance and methods type info
// generated from actor.
// Deprecated: use DefaultActorContainerContext instead.
type DefaultActorContainer struct {
	//nolint:staticcheck
	actor actor.Server
	ctx   *DefaultActorContainerContext
}

// DefaultActorContainerContext contains actor instance and methods type info
// generated from actor.
type DefaultActorContainerContext struct {
	methodType map[string]*MethodType
	actor      actor.ServerContext
	serializer codec.Codec
}

// NewDefaultActorContainer creates a new ActorContainer with provider impl actor and serializer.
// Deprecated: use NewDefaultActorContainerContext instead.
//
//nolint:staticcheck
func NewDefaultActorContainer(actorID string, impl actor.Server, serializer codec.Codec) (ActorContainer, actorErr.ActorErr) {
	ctx, err := NewDefaultActorContainerContext(context.Background(), actorID, impl.WithContext(), serializer)
	return &DefaultActorContainer{ctx: ctx.(*DefaultActorContainerContext), actor: impl}, err
}

// Deprecated: use NewDefaultActorContainerContext instead.
func (d *DefaultActorContainer) GetActor() actor.Server {
	return d.actor
}

// Invoke call actor method with given methodName and param.
// Deprecated: use NewDefaultActorContainerContext instead.
func (d *DefaultActorContainer) Invoke(methodName string, param []byte) ([]reflect.Value, actorErr.ActorErr) {
	return d.ctx.Invoke(context.Background(), methodName, param)
}

// NewDefaultActorContainerContext is the same as NewDefaultActorContainer, but with initial context.
func NewDefaultActorContainerContext(ctx context.Context, actorID string, impl actor.ServerContext, serializer codec.Codec) (ActorContainerContext, actorErr.ActorErr) {
	impl.SetID(actorID)
	daprClient, _ := dapr.NewClient()
	// create state manager for this new actor
	impl.SetStateManager(state.NewActorStateManagerContext(impl.Type(), actorID, state.NewDaprStateAsyncProvider(daprClient)))
	// save state of this actor
	err := impl.SaveState(ctx)
	if err != nil {
		return nil, actorErr.ErrSaveStateFailed
	}
	methodType, err := getAbsctractMethodMap(impl)
	if err != nil {
		log.Printf("failed to get absctract method map from registered provider, err = %s", err)
		return nil, actorErr.ErrActorServerInvalid
	}
	return &DefaultActorContainerContext{
		methodType: methodType,
		actor:      impl,
		serializer: serializer,
	}, actorErr.Success
}

// Invoke call actor method with given context, methodName and param.
func (d *DefaultActorContainerContext) Invoke(ctx context.Context, methodName string, param []byte) ([]reflect.Value, actorErr.ActorErr) {
	methodType, ok := d.methodType[methodName]
	if !ok {
		return nil, actorErr.ErrActorMethodNoFound
	}
	argsValues := make([]reflect.Value, 0)
	argsValues = append(argsValues, reflect.ValueOf(d.actor), reflect.ValueOf(ctx))
	if len(methodType.argsType) > 0 {
		typ := methodType.argsType[0]
		paramValue := reflect.New(typ)
		paramInterface := paramValue.Interface()
		if err := d.serializer.Unmarshal(param, paramInterface); err != nil {
			return nil, actorErr.ErrActorMethodSerializeFailed
		}
		argsValues = append(argsValues, reflect.ValueOf(paramInterface).Elem())
	}
	returnValue := methodType.method.Func.Call(argsValues)
	return returnValue, actorErr.Success
}

func (d *DefaultActorContainerContext) GetActor() actor.ServerContext {
	return d.actor
}
