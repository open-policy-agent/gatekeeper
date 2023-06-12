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

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	anypb "github.com/golang/protobuf/ptypes/any"
	"github.com/pkg/errors"

	"github.com/dapr/go-sdk/actor"
	"github.com/dapr/go-sdk/actor/codec"
	"github.com/dapr/go-sdk/actor/config"
	pb "github.com/dapr/go-sdk/dapr/proto/runtime/v1"
)

type InvokeActorRequest struct {
	ActorType string
	ActorID   string
	Method    string
	Data      []byte
}

type InvokeActorResponse struct {
	Data []byte
}

// InvokeActor invokes specific operation on the configured Dapr binding.
// This method covers input, output, and bi-directional bindings.
func (c *GRPCClient) InvokeActor(ctx context.Context, in *InvokeActorRequest) (out *InvokeActorResponse, err error) {
	if in == nil {
		return nil, errors.New("actor invocation required")
	}
	if in.Method == "" {
		return nil, errors.New("actor invocation method required")
	}
	if in.ActorType == "" {
		return nil, errors.New("actor invocation actorType required")
	}
	if in.ActorID == "" {
		return nil, errors.New("actor invocation actorID required")
	}

	req := &pb.InvokeActorRequest{
		ActorType: in.ActorType,
		ActorId:   in.ActorID,
		Method:    in.Method,
		Data:      in.Data,
	}

	resp, err := c.protoClient.InvokeActor(c.withAuthToken(ctx), req)
	if err != nil {
		return nil, errors.Wrapf(err, "error invoking binding %s/%s", in.ActorType, in.ActorID)
	}

	out = &InvokeActorResponse{}

	if resp != nil {
		out.Data = resp.Data
	}

	return out, nil
}

// ImplActorClientStub impls the given client stub @actorClientStub, an example of client stub is as followed
/*
type ClientStub struct {
// User defined function
	GetUser       func(context.Context, *User) (*User, error)
	Invoke        func(context.Context, string) (string, error)
	Get           func(context.Context) (string, error)
	Post          func(context.Context, string) error
	StartTimer    func(context.Context, *TimerRequest) error
	StopTimer     func(context.Context, *TimerRequest) error
	...
}

// Type defined the target type, which should be compatible with server side actor
func (a *ClientStub) Type() string {
	return "testActorType"
}

// ID defined actor ID to be invoked
func (a *ClientStub) ID() string {
	return "ActorImplID123456"
}.
*/
func (c *GRPCClient) ImplActorClientStub(actorClientStub actor.Client, opt ...config.Option) {
	serializerType := config.GetConfigFromOptions(opt...).SerializerType
	serializer, err := codec.GetActorCodec(serializerType)
	if err != nil {
		fmt.Printf("[Actor] ERROR: serializer type %s unsupported\n", serializerType)
		return
	}

	c.implActor(actorClientStub, serializer)
}

type RegisterActorReminderRequest struct {
	ActorType string
	ActorID   string
	Name      string
	DueTime   string
	Period    string
	TTL       string
	Data      []byte
}

// RegisterActorReminder registers a new reminder to target actor. Then, a reminder would be created and
// invoke actor's ReminderCall function if implemented.
// If server side actor impls this function, it's asserted to actor.ReminderCallee and can be invoked with call period
// and state data as param @in defined.
// Scheduling parameters 'DueTime', 'Period', and 'TTL' are optional.
func (c *GRPCClient) RegisterActorReminder(ctx context.Context, in *RegisterActorReminderRequest) (err error) {
	if in == nil {
		return errors.New("actor register reminder invocation request param required")
	}
	if in.ActorType == "" {
		return errors.New("actor register reminder invocation actorType required")
	}
	if in.ActorID == "" {
		return errors.New("actor register reminder invocation actorID required")
	}
	if in.Name == "" {
		return errors.New("actor register reminder invocation name required")
	}

	req := &pb.RegisterActorReminderRequest{
		ActorType: in.ActorType,
		ActorId:   in.ActorID,
		Name:      in.Name,
		DueTime:   in.DueTime,
		Period:    in.Period,
		Ttl:       in.TTL,
		Data:      in.Data,
	}

	_, err = c.protoClient.RegisterActorReminder(c.withAuthToken(ctx), req)
	if err != nil {
		return errors.Wrapf(err, "error invoking register actor reminder %s/%s", in.ActorType, in.ActorID)
	}
	return nil
}

type UnregisterActorReminderRequest struct {
	ActorType string
	ActorID   string
	Name      string
}

// UnregisterActorReminder would unregister the actor reminder.
func (c *GRPCClient) UnregisterActorReminder(ctx context.Context, in *UnregisterActorReminderRequest) error {
	if in == nil {
		return errors.New("actor unregister reminder invocation request param required")
	}
	if in.ActorType == "" {
		return errors.New("actor unregister reminder invocation actorType required")
	}
	if in.ActorID == "" {
		return errors.New("actor unregister reminder invocation actorID required")
	}
	if in.Name == "" {
		return errors.New("actor unregister reminder invocation name required")
	}

	req := &pb.UnregisterActorReminderRequest{
		ActorType: in.ActorType,
		ActorId:   in.ActorID,
		Name:      in.Name,
	}

	_, err := c.protoClient.UnregisterActorReminder(c.withAuthToken(ctx), req)
	if err != nil {
		return errors.Wrapf(err, "error invoking unregister actor reminder %s/%s", in.ActorType, in.ActorID)
	}
	return nil
}

type RenameActorReminderRequest struct {
	OldName   string
	ActorType string
	ActorID   string
	NewName   string
}

// RenameActorReminder would rename the actor reminder.
func (c *GRPCClient) RenameActorReminder(ctx context.Context, in *RenameActorReminderRequest) error {
	if in == nil {
		return errors.New("actor rename reminder invocation request param required")
	}
	if in.ActorType == "" {
		return errors.New("actor rename reminder invocation actorType required")
	}
	if in.ActorID == "" {
		return errors.New("actor rename reminder invocation actorID required")
	}
	if in.OldName == "" {
		return errors.New("actor rename reminder invocation oldName required")
	}
	if in.NewName == "" {
		return errors.New("actor rename reminder invocation newName required")
	}

	req := &pb.RenameActorReminderRequest{
		ActorType: in.ActorType,
		ActorId:   in.ActorID,
		OldName:   in.OldName,
		NewName:   in.NewName,
	}

	_, err := c.protoClient.RenameActorReminder(c.withAuthToken(ctx), req)
	if err != nil {
		return errors.Wrapf(err, "error invoking rename actor reminder %s/%s", in.ActorType, in.ActorID)
	}
	return nil
}

type RegisterActorTimerRequest struct {
	ActorType string
	ActorID   string
	Name      string
	DueTime   string
	Period    string
	TTL       string
	Data      []byte
	CallBack  string
}

// RegisterActorTimer register actor timer as given param @in defined.
// Scheduling parameters 'DueTime', 'Period', and 'TTL' are optional.
func (c *GRPCClient) RegisterActorTimer(ctx context.Context, in *RegisterActorTimerRequest) (err error) {
	if in == nil {
		return errors.New("actor register timer invocation request param required")
	}
	if in.ActorType == "" {
		return errors.New("actor register timer invocation actorType required")
	}
	if in.ActorID == "" {
		return errors.New("actor register timer invocation actorID required")
	}
	if in.Name == "" {
		return errors.New("actor register timer invocation name required")
	}
	if in.CallBack == "" {
		return errors.New("actor register timer invocation callback function required")
	}

	req := &pb.RegisterActorTimerRequest{
		ActorType: in.ActorType,
		ActorId:   in.ActorID,
		Name:      in.Name,
		DueTime:   in.DueTime,
		Period:    in.Period,
		Ttl:       in.TTL,
		Data:      in.Data,
		Callback:  in.CallBack,
	}

	_, err = c.protoClient.RegisterActorTimer(c.withAuthToken(ctx), req)
	if err != nil {
		return errors.Wrapf(err, "error invoking actor register timer %s/%s", in.ActorType, in.ActorID)
	}

	return nil
}

type UnregisterActorTimerRequest struct {
	ActorType string
	ActorID   string
	Name      string
}

// UnregisterActorTimer unregisters actor timer.
func (c *GRPCClient) UnregisterActorTimer(ctx context.Context, in *UnregisterActorTimerRequest) error {
	if in == nil {
		return errors.New("actor unregister timer invocation request param required")
	}
	if in.ActorType == "" {
		return errors.New("actor unregister timer invocation actorType required")
	}
	if in.ActorID == "" {
		return errors.New("actor unregister timer invocation actorID required")
	}
	if in.Name == "" {
		return errors.New("actor unregister timer invocation name required")
	}
	req := &pb.UnregisterActorTimerRequest{
		ActorType: in.ActorType,
		ActorId:   in.ActorID,
		Name:      in.Name,
	}

	_, err := c.protoClient.UnregisterActorTimer(c.withAuthToken(ctx), req)
	if err != nil {
		return errors.Wrapf(err, "error invoking binding %s/%s", in.ActorType, in.ActorID)
	}

	return nil
}

func (c *GRPCClient) implActor(actor actor.Client, serializer codec.Codec) {
	actorValue := reflect.ValueOf(actor)
	valueOfActor := actorValue.Elem()
	typeOfActor := valueOfActor.Type()

	// check incoming interface, the incoming interface's elem must be a struct.
	if typeOfActor.Kind() != reflect.Struct {
		fmt.Println("[Actor] ERROR: impl actor client stub failed, incoming interface is not struct")
		return
	}

	numField := valueOfActor.NumField()
	for i := 0; i < numField; i++ {
		t := typeOfActor.Field(i)
		methodName := t.Name
		if methodName == "Type" {
			continue
		}
		f := valueOfActor.Field(i)
		if f.Kind() == reflect.Func && f.IsValid() && f.CanSet() {
			outNum := t.Type.NumOut()

			if outNum != 1 && outNum != 2 {
				fmt.Printf("[Actor] ERROR: method %s of mtype %v has wrong number of in out parameters %d; needs exactly 1/2\n",
					t.Name, t.Type.String(), outNum)
				continue
			}

			// The latest return type of the method must be error.
			if returnType := t.Type.Out(outNum - 1); returnType != reflect.Zero(reflect.TypeOf((*error)(nil)).Elem()).Type() {
				fmt.Printf("[Actor] ERROR: the latest return type %s of method %q is not error\n", returnType, t.Name)
				continue
			}

			funcOuts := make([]reflect.Type, outNum)
			for i := 0; i < outNum; i++ {
				funcOuts[i] = t.Type.Out(i)
			}

			f.Set(reflect.MakeFunc(f.Type(), c.makeCallProxyFunction(actor, methodName, funcOuts, serializer)))
		}
	}
}

func (c *GRPCClient) makeCallProxyFunction(actor actor.Client, methodName string, outs []reflect.Type, serializer codec.Codec) func(in []reflect.Value) []reflect.Value {
	return func(in []reflect.Value) []reflect.Value {
		var (
			err    error
			inIArr []interface{}
			reply  reflect.Value
		)

		if len(outs) == 2 {
			if outs[0].Kind() == reflect.Ptr {
				reply = reflect.New(outs[0].Elem())
			} else {
				reply = reflect.New(outs[0])
			}
		}

		start := 0
		end := len(in)
		invCtx := context.Background()
		if end > 0 {
			if in[0].Type().String() == "context.Context" {
				if !in[0].IsNil() {
					invCtx = in[0].Interface().(context.Context)
				}
				start++
			}
		}

		if end-start <= 0 {
			inIArr = []interface{}{}
		} else if end-start == 1 {
			inIArr = []interface{}{in[start].Interface()}
		} else {
			fmt.Println("[Actor] ERROR: param nums is zero or one is allowed by actor")
			return nil
		}

		var data []byte
		if len(inIArr) > 0 {
			data, err = json.Marshal(inIArr[0])
		}
		if err != nil {
			panic(err)
		}

		rsp, err := c.InvokeActor(invCtx, &InvokeActorRequest{
			ActorType: actor.Type(),
			ActorID:   actor.ID(),
			Method:    methodName,
			Data:      data,
		})

		if len(outs) == 1 {
			return []reflect.Value{reflect.ValueOf(&err).Elem()}
		}

		response := reply.Interface()
		if rsp != nil {
			if err = serializer.Unmarshal(rsp.Data, response); err != nil {
				fmt.Printf("[Actor] ERROR: unmarshal response err = %v\n", err)
			}
		}
		if len(outs) == 2 && outs[0].Kind() != reflect.Ptr {
			return []reflect.Value{reply.Elem(), reflect.ValueOf(&err).Elem()}
		}
		return []reflect.Value{reply, reflect.ValueOf(&err).Elem()}
	}
}

type GetActorStateRequest struct {
	ActorType string
	ActorID   string
	KeyName   string
}

type GetActorStateResponse struct {
	Data []byte
}

func (c *GRPCClient) GetActorState(ctx context.Context, in *GetActorStateRequest) (*GetActorStateResponse, error) {
	if in == nil {
		return nil, errors.New("actor get state invocation request param required")
	}
	if in.ActorType == "" {
		return nil, errors.New("actor get state invocation actorType required")
	}
	if in.ActorID == "" {
		return nil, errors.New("actor get state invocation actorID required")
	}
	if in.KeyName == "" {
		return nil, errors.New("actor get state invocation keyName required")
	}
	rsp, err := c.protoClient.GetActorState(c.withAuthToken(ctx), &pb.GetActorStateRequest{
		ActorId:   in.ActorID,
		ActorType: in.ActorType,
		Key:       in.KeyName,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "error invoking actor get state %s/%s", in.ActorType, in.ActorID)
	}
	return &GetActorStateResponse{Data: rsp.Data}, nil
}

type ActorStateOperation struct {
	OperationType string
	Key           string
	Value         []byte
}

func (c *GRPCClient) SaveStateTransactionally(ctx context.Context, actorType, actorID string, operations []*ActorStateOperation) error {
	if len(operations) == 0 {
		return errors.New("actor save state transactionally invocation request param operations is empty")
	}
	if actorType == "" {
		return errors.New("actor save state transactionally invocation actorType required")
	}
	if actorID == "" {
		return errors.New("actor save state transactionally invocation actorID required")
	}
	grpcOperations := make([]*pb.TransactionalActorStateOperation, 0)
	for _, op := range operations {
		grpcOperations = append(grpcOperations, &pb.TransactionalActorStateOperation{
			OperationType: op.OperationType,
			Key:           op.Key,
			Value: &anypb.Any{
				Value: op.Value,
			},
		})
	}
	_, err := c.protoClient.ExecuteActorStateTransaction(c.withAuthToken(ctx), &pb.ExecuteActorStateTransactionRequest{
		ActorType:  actorType,
		ActorId:    actorID,
		Operations: grpcOperations,
	})
	return err
}
