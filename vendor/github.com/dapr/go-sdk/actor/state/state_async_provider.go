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

import (
	"context"

	"github.com/pkg/errors"

	"github.com/dapr/go-sdk/actor/codec"
	"github.com/dapr/go-sdk/actor/codec/constant"
	client "github.com/dapr/go-sdk/client"
)

type DaprStateAsyncProvider struct {
	daprClient      client.Client
	stateSerializer codec.Codec
}

func (d *DaprStateAsyncProvider) Contains(actorType string, actorID string, stateName string) (bool, error) {
	result, err := d.daprClient.GetActorState(context.Background(), &client.GetActorStateRequest{
		ActorType: actorType,
		ActorID:   actorID,
		KeyName:   stateName,
	})
	if err != nil || result == nil {
		return false, err
	}
	return len(result.Data) > 0, err
}

func (d *DaprStateAsyncProvider) Load(actorType, actorID, stateName string, reply interface{}) error {
	result, err := d.daprClient.GetActorState(context.Background(), &client.GetActorStateRequest{
		ActorType: actorType,
		ActorID:   actorID,
		KeyName:   stateName,
	})
	if err != nil {
		return errors.Errorf("get actor state error = %s", err.Error())
	}
	if len(result.Data) == 0 {
		return errors.Errorf("get actor state result empty, with actorType: %s, actorID: %s, stateName %s", actorType, actorID, stateName)
	}
	if err := d.stateSerializer.Unmarshal(result.Data, reply); err != nil {
		return errors.Errorf("unmarshal state data error = %s", err.Error())
	}
	return nil
}

func (d *DaprStateAsyncProvider) Apply(actorType, actorID string, changes []*ActorStateChange) error {
	if len(changes) == 0 {
		return nil
	}

	operations := make([]*client.ActorStateOperation, 0)
	var value []byte
	for _, stateChange := range changes {
		if stateChange == nil {
			continue
		}

		daprOperationName := string(stateChange.changeKind)
		if len(daprOperationName) == 0 {
			continue
		}

		if stateChange.changeKind == Add {
			data, err := d.stateSerializer.Marshal(stateChange.value)
			if err != nil {
				return err
			}
			value = data
		}
		operations = append(operations, &client.ActorStateOperation{
			OperationType: daprOperationName,
			Key:           stateChange.stateName,
			Value:         value,
		})
	}

	if len(operations) == 0 {
		return nil
	}

	return d.daprClient.SaveStateTransactionally(context.Background(), actorType, actorID, operations)
}

// TODO(@laurence) the daprClient may be nil.
func NewDaprStateAsyncProvider(daprClient client.Client) *DaprStateAsyncProvider {
	stateSerializer, _ := codec.GetActorCodec(constant.DefaultSerializerType)
	return &DaprStateAsyncProvider{
		stateSerializer: stateSerializer,
		daprClient:      daprClient,
	}
}
