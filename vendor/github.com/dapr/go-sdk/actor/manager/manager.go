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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/dapr/go-sdk/actor"
	"github.com/dapr/go-sdk/actor/api"
	"github.com/dapr/go-sdk/actor/codec"
	actorErr "github.com/dapr/go-sdk/actor/error"
)

type ActorManager interface {
	RegisterActorImplFactory(f actor.Factory)
	InvokeMethod(actorID, methodName string, request []byte) ([]byte, actorErr.ActorErr)
	DeactivateActor(actorID string) actorErr.ActorErr
	InvokeReminder(actorID, reminderName string, params []byte) actorErr.ActorErr
	InvokeTimer(actorID, timerName string, params []byte) actorErr.ActorErr
}

type ActorManagerContext interface {
	RegisterActorImplFactory(f actor.FactoryContext)
	InvokeMethod(ctx context.Context, actorID, methodName string, request []byte) ([]byte, actorErr.ActorErr)
	DeactivateActor(ctx context.Context, actorID string) actorErr.ActorErr
	InvokeReminder(ctx context.Context, actorID, reminderName string, params []byte) actorErr.ActorErr
	InvokeTimer(ctx context.Context, actorID, timerName string, params []byte) actorErr.ActorErr
}

// DefaultActorManagerContext is to manage one type of actor.
type DefaultActorManagerContext struct {
	// factory is the actor factory of specific type of actor
	factory actor.FactoryContext

	// activeActors stores the map actorID -> ActorContainer
	activeActors sync.Map

	// serializer is the param and response serializer of the actor
	serializer codec.Codec
}

// DefaultActorManager is to manage one type of actor.
// Deprecated: use DefaultActorManagerContext instead.
type DefaultActorManager struct {
	ctx ActorManagerContext
}

// Deprecated: use DefaultActorManagerContext instead.
func NewDefaultActorManager(serializerType string) (ActorManager, actorErr.ActorErr) {
	ctx, err := NewDefaultActorManagerContext(serializerType)
	return &DefaultActorManager{ctx: ctx}, err
}

// Deprecated: use DefaultActorManagerContext instead.
func (m *DefaultActorManager) RegisterActorImplFactory(f actor.Factory) {
	m.ctx.RegisterActorImplFactory(func() actor.ServerContext { return f().WithContext() })
}

// Deprecated: use DefaultActorManagerContext instead.
func (m *DefaultActorManager) InvokeMethod(actorID, methodName string, request []byte) ([]byte, actorErr.ActorErr) {
	return m.ctx.InvokeMethod(context.Background(), actorID, methodName, request)
}

// Deprecated: use DefaultActorManagerContext instead.
func (m *DefaultActorManager) DeactivateActor(actorID string) actorErr.ActorErr {
	return m.ctx.DeactivateActor(context.Background(), actorID)
}

// Deprecated: use DefaultActorManagerContext instead.
func (m *DefaultActorManager) InvokeReminder(actorID, reminderName string, params []byte) actorErr.ActorErr {
	return m.ctx.InvokeReminder(context.Background(), actorID, reminderName, params)
}

// Deprecated: use DefaultActorManagerContext instead.
func (m *DefaultActorManager) InvokeTimer(actorID, timerName string, params []byte) actorErr.ActorErr {
	return m.ctx.InvokeTimer(context.Background(), actorID, timerName, params)
}

func NewDefaultActorManagerContext(serializerType string) (ActorManagerContext, actorErr.ActorErr) {
	serializer, err := codec.GetActorCodec(serializerType)
	if err != nil {
		return nil, actorErr.ErrActorSerializeNoFound
	}
	return &DefaultActorManagerContext{
		serializer: serializer,
	}, actorErr.Success
}

// RegisterActorImplFactory registers the action factory f.
func (m *DefaultActorManagerContext) RegisterActorImplFactory(f actor.FactoryContext) {
	m.factory = f
}

// getAndCreateActorContainerIfNotExist will.
func (m *DefaultActorManagerContext) getAndCreateActorContainerIfNotExist(ctx context.Context, actorID string) (ActorContainerContext, actorErr.ActorErr) {
	val, ok := m.activeActors.Load(actorID)
	if !ok {
		newContainer, aerr := NewDefaultActorContainerContext(ctx, actorID, m.factory(), m.serializer)
		if aerr != actorErr.Success {
			return nil, aerr
		}
		m.activeActors.Store(actorID, newContainer)
		val, _ = m.activeActors.Load(actorID)
	}
	return val.(ActorContainerContext), actorErr.Success
}

// InvokeMethod to invoke local function by @actorID, @methodName and @request request param.
func (m *DefaultActorManagerContext) InvokeMethod(ctx context.Context, actorID, methodName string, request []byte) ([]byte, actorErr.ActorErr) {
	if m.factory == nil {
		return nil, actorErr.ErrActorFactoryNotSet
	}

	actorContainer, aerr := m.getAndCreateActorContainerIfNotExist(ctx, actorID)
	if aerr != actorErr.Success {
		return nil, aerr
	}
	returnValue, aerr := actorContainer.Invoke(ctx, methodName, request)
	if aerr != actorErr.Success {
		return nil, aerr
	}
	if len(returnValue) == 1 {
		return nil, actorErr.Success
	}

	var (
		retErr interface{}
		replyv reflect.Value
	)

	if len(returnValue) == 2 {
		replyv = returnValue[0]
		retErr = returnValue[1].Interface()
	}

	if retErr != nil {
		return nil, actorErr.ErrActorInvokeFailed
	}
	rspData, err := m.serializer.Marshal(replyv.Interface())
	if err != nil {
		return nil, actorErr.ErrActorMethodSerializeFailed
	}
	if err := actorContainer.GetActor().SaveState(ctx); err != nil {
		return nil, actorErr.ErrSaveStateFailed
	}
	return rspData, actorErr.Success
}

// DeactivateActor removes actor from actor manager.
func (m *DefaultActorManagerContext) DeactivateActor(_ context.Context, actorID string) actorErr.ActorErr {
	_, ok := m.activeActors.Load(actorID)
	if !ok {
		return actorErr.ErrActorIDNotFound
	}
	m.activeActors.Delete(actorID)
	return actorErr.Success
}

// InvokeReminder invoke reminder function with given params.
func (m *DefaultActorManagerContext) InvokeReminder(ctx context.Context, actorID, reminderName string, params []byte) actorErr.ActorErr {
	if m.factory == nil {
		return actorErr.ErrActorFactoryNotSet
	}
	reminderParams := &api.ActorReminderParams{}
	if err := json.Unmarshal(params, reminderParams); err != nil {
		log.Printf("failed to unmarshal reminder param, err: %v ", err)
		return actorErr.ErrRemindersParamsInvalid
	}
	actorContainer, aerr := m.getAndCreateActorContainerIfNotExist(ctx, actorID)
	if aerr != actorErr.Success {
		return aerr
	}

	targetActor, ok := actorContainer.GetActor().(actor.ReminderCallee)
	if !ok {
		return actorErr.ErrReminderFuncUndefined
	}
	targetActor.ReminderCall(reminderName, reminderParams.Data, reminderParams.DueTime, reminderParams.Period)
	return actorErr.Success
}

// InvokeTimer invoke timer callback function with given params.
func (m *DefaultActorManagerContext) InvokeTimer(ctx context.Context, actorID, timerName string, params []byte) actorErr.ActorErr {
	if m.factory == nil {
		return actorErr.ErrActorFactoryNotSet
	}
	timerParams := &api.ActorTimerParam{}
	if err := json.Unmarshal(params, timerParams); err != nil {
		log.Printf("failed to unmarshal reminder param, err: %v ", err)
		return actorErr.ErrTimerParamsInvalid
	}
	actorContainer, aerr := m.getAndCreateActorContainerIfNotExist(ctx, actorID)
	if aerr != actorErr.Success {
		return aerr
	}
	_, aerr = actorContainer.Invoke(ctx, timerParams.CallBack, timerParams.Data)
	return aerr
}

func getAbsctractMethodMap(rcvr interface{}) (map[string]*MethodType, error) {
	s := &Service{}
	s.reflectType = reflect.TypeOf(rcvr)
	s.reflctValue = reflect.ValueOf(rcvr)
	sname := reflect.Indirect(s.reflctValue).Type().Name()
	if !isExported(sname) {
		return nil, fmt.Errorf("type %s is not exported", sname)
	}
	return suitableMethods(s.reflectType), nil
}

func isExported(name string) bool {
	s, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(s)
}

// Service is description of service.
type Service struct {
	reflctValue reflect.Value
	reflectType reflect.Type
}

// MethodType is description of service method.
type MethodType struct {
	method    reflect.Method
	ctxType   reflect.Type   // request context
	argsType  []reflect.Type // args except ctx, include replyType if existing
	replyType reflect.Type   // return value, otherwise it is nil
}

// suitableMethods returns suitable Rpc methods of typ.
func suitableMethods(typ reflect.Type) map[string]*MethodType {
	methods := make(map[string]*MethodType)
	for m := 0; m < typ.NumMethod(); m++ {
		method := typ.Method(m)
		if mt, err := suiteMethod(method); err != nil {
			log.Printf("method %s is illegal, err = %s, just skip it", method.Name, err)
		} else {
			methods[method.Name] = mt
		}
	}
	return methods
}

// suiteMethod returns a suitable Rpc methodType.
func suiteMethod(method reflect.Method) (*MethodType, error) {
	mtype := method.Type
	mname := method.Name
	inNum := mtype.NumIn()
	outNum := mtype.NumOut()

	// Method must be exported.
	if method.PkgPath != "" {
		return nil, errors.New("method is not exported")
	}

	var (
		replyType, ctxType reflect.Type
		argsType           []reflect.Type
	)

	if outNum > 2 || outNum == 0 {
		return nil, errors.New("num out invalid")
	}

	// The latest return type of the method must be error.
	if returnType := mtype.Out(outNum - 1); returnType != typeOfError {
		return nil, fmt.Errorf("the latest return type %s of method %q is not error", returnType, mname)
	}

	// replyType
	if outNum == 2 {
		replyType = mtype.Out(0)
		if !isExportedOrBuiltinType(replyType) {
			return nil, fmt.Errorf("reply type of method %s not exported{%v}", mname, replyType)
		}
	}

	index := 1

	// ctxType
	if inNum > 1 && mtype.In(1).String() == "context.Context" {
		ctxType = mtype.In(1)
		index = 2
	}

	for ; index < inNum; index++ {
		argsType = append(argsType, mtype.In(index))
		// need not be a pointer.
		if !isExportedOrBuiltinType(mtype.In(index)) {
			return nil, fmt.Errorf("argument type of method %q is not exported %v", mname, mtype.In(index))
		}
	}

	return &MethodType{method: method, argsType: argsType, replyType: replyType, ctxType: ctxType}, nil
}

var typeOfError = reflect.TypeOf((*error)(nil)).Elem()

func isExportedOrBuiltinType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return isExported(t.Name()) || t.PkgPath() == ""
}
