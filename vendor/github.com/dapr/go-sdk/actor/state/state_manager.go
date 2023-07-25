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
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/dapr/go-sdk/actor"
)

type stateManager struct {
	*stateManagerCtx
}

type stateManagerCtx struct {
	actorTypeName      string
	actorID            string
	stateChangeTracker sync.Map // map[string]*ChangeMetadata
	stateAsyncProvider *DaprStateAsyncProvider
}

// Deprecated: use NewActorStateManagerContext instead.
func (s *stateManager) Add(stateName string, value any) error {
	return s.stateManagerCtx.Add(context.Background(), stateName, value)
}

// Deprecated: use NewActorStateManagerContext instead.
func (s *stateManager) Get(stateName string, reply any) error {
	return s.stateManagerCtx.Get(context.Background(), stateName, reply)
}

// Deprecated: use NewActorStateManagerContext instead.
func (s *stateManager) Set(stateName string, value any) error {
	return s.stateManagerCtx.Set(context.Background(), stateName, value)
}

// Deprecated: use NewActorStateManagerContext instead.
func (s *stateManager) Remove(stateName string) error {
	return s.stateManagerCtx.Remove(context.Background(), stateName)
}

// Deprecated: use NewActorStateManagerContext instead.
func (s *stateManager) Contains(stateName string) (bool, error) {
	return s.stateManagerCtx.Contains(context.Background(), stateName)
}

// Deprecated: use NewActorStateManagerContext instead.
func (s *stateManager) Save() error {
	return s.stateManagerCtx.Save(context.Background())
}

// Deprecated: use NewActorStateManagerContext instead.
func (s *stateManager) Flush() {
	s.stateManagerCtx.Flush(context.Background())
}

// Deprecated: use NewActorStateManagerContext instead.
func (s *stateManager) WithContext() actor.StateManagerContext {
	return s.stateManagerCtx
}

func (s *stateManagerCtx) Add(ctx context.Context, stateName string, value any) error {
	if stateName == "" {
		return errors.New("state name can't be empty")
	}
	exists, err := s.stateAsyncProvider.ContainsContext(ctx, s.actorTypeName, s.actorID, stateName)
	if err != nil {
		return err
	}

	if val, ok := s.stateChangeTracker.Load(stateName); ok {
		metadata := val.(*ChangeMetadata)
		if metadata.Kind == Remove {
			s.stateChangeTracker.Store(stateName, &ChangeMetadata{
				Kind:  Update,
				Value: value,
			})
			return nil
		}
		return fmt.Errorf("duplicate cached state: %s", stateName)
	}
	if exists {
		return fmt.Errorf("duplicate state: %s", stateName)
	}
	s.stateChangeTracker.Store(stateName, &ChangeMetadata{
		Kind:  Add,
		Value: value,
	})
	return nil
}

func (s *stateManagerCtx) Get(ctx context.Context, stateName string, reply any) error {
	if stateName == "" {
		return errors.New("state name can't be empty")
	}

	if val, ok := s.stateChangeTracker.Load(stateName); ok {
		metadata := val.(*ChangeMetadata)
		if metadata.Kind == Remove {
			return fmt.Errorf("state is marked for removal: %s", stateName)
		}
		replyVal := reflect.ValueOf(reply).Elem()
		metadataValue := reflect.ValueOf(metadata.Value)
		if metadataValue.Kind() == reflect.Ptr {
			replyVal.Set(metadataValue.Elem())
		} else {
			replyVal.Set(metadataValue)
		}

		return nil
	}

	err := s.stateAsyncProvider.LoadContext(ctx, s.actorTypeName, s.actorID, stateName, reply)
	s.stateChangeTracker.Store(stateName, &ChangeMetadata{
		Kind:  None,
		Value: reply,
	})
	return err
}

func (s *stateManagerCtx) Set(_ context.Context, stateName string, value any) error {
	if stateName == "" {
		return errors.New("state name can't be empty")
	}
	if val, ok := s.stateChangeTracker.Load(stateName); ok {
		metadata := val.(*ChangeMetadata)
		if metadata.Kind == None || metadata.Kind == Remove {
			metadata.Kind = Update
		}
		s.stateChangeTracker.Store(stateName, NewChangeMetadata(metadata.Kind, value))
		return nil
	}
	s.stateChangeTracker.Store(stateName, &ChangeMetadata{
		Kind:  Add,
		Value: value,
	})
	return nil
}

func (s *stateManagerCtx) SetWithTTL(_ context.Context, stateName string, value any, ttl time.Duration) error {
	if stateName == "" {
		return errors.New("state name can't be empty")
	}

	if ttl < 0 {
		return errors.New("ttl can't be negative")
	}

	if val, ok := s.stateChangeTracker.Load(stateName); ok {
		metadata := val.(*ChangeMetadata)
		if metadata.Kind == None || metadata.Kind == Remove {
			metadata.Kind = Update
		}
		s.stateChangeTracker.Store(stateName, NewChangeMetadata(metadata.Kind, value))
		return nil
	}
	s.stateChangeTracker.Store(stateName, (&ChangeMetadata{
		Kind:  Add,
		Value: value,
	}).WithTTL(ttl))
	return nil
}

func (s *stateManagerCtx) Remove(ctx context.Context, stateName string) error {
	if stateName == "" {
		return errors.New("state name can't be empty")
	}
	if val, ok := s.stateChangeTracker.Load(stateName); ok {
		metadata := val.(*ChangeMetadata)
		if metadata.Kind == Remove {
			return nil
		}
		if metadata.Kind == Add {
			s.stateChangeTracker.Delete(stateName)
			return nil
		}

		s.stateChangeTracker.Store(stateName, &ChangeMetadata{
			Kind:  Remove,
			Value: nil,
		})
		return nil
	}
	if exist, err := s.stateAsyncProvider.ContainsContext(ctx, s.actorTypeName, s.actorID, stateName); err != nil && exist {
		s.stateChangeTracker.Store(stateName, &ChangeMetadata{
			Kind:  Remove,
			Value: nil,
		})
	}
	return nil
}

func (s *stateManagerCtx) Contains(ctx context.Context, stateName string) (bool, error) {
	if stateName == "" {
		return false, errors.New("state name can't be empty")
	}
	if val, ok := s.stateChangeTracker.Load(stateName); ok {
		metadata := val.(*ChangeMetadata)
		if metadata.Kind == Remove {
			return false, nil
		}
		return true, nil
	}
	return s.stateAsyncProvider.ContainsContext(ctx, s.actorTypeName, s.actorID, stateName)
}

func (s *stateManagerCtx) Save(ctx context.Context) error {
	changes := make([]*ActorStateChange, 0)
	s.stateChangeTracker.Range(func(key, value any) bool {
		stateName := key.(string)
		metadata := value.(*ChangeMetadata)
		changes = append(changes, NewActorStateChange(stateName, metadata.Value, metadata.Kind, metadata.TTL))
		return true
	})
	if err := s.stateAsyncProvider.ApplyContext(ctx, s.actorTypeName, s.actorID, changes); err != nil {
		return err
	}
	s.Flush(ctx)
	return nil
}

func (s *stateManagerCtx) Flush(_ context.Context) {
	s.stateChangeTracker.Range(func(key, value any) bool {
		stateName := key.(string)
		metadata := value.(*ChangeMetadata)
		if metadata.Kind == Remove {
			s.stateChangeTracker.Delete(stateName)
			return true
		}
		metadata = NewChangeMetadata(None, metadata.Value)
		s.stateChangeTracker.Store(stateName, metadata)
		return true
	})
}

// Deprecated: use NewActorStateManagerContext instead.
func NewActorStateManager(actorTypeName string, actorID string, provider *DaprStateAsyncProvider) actor.StateManager {
	return &stateManager{
		stateManagerCtx: &stateManagerCtx{
			stateAsyncProvider: provider,
			actorTypeName:      actorTypeName,
			actorID:            actorID,
		},
	}
}

func NewActorStateManagerContext(actorTypeName string, actorID string, provider *DaprStateAsyncProvider) actor.StateManagerContext {
	return &stateManagerCtx{
		stateAsyncProvider: provider,
		actorTypeName:      actorTypeName,
		actorID:            actorID,
	}
}
