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
	"errors"
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes/duration"

	v1 "github.com/dapr/go-sdk/dapr/proto/common/v1"
	pb "github.com/dapr/go-sdk/dapr/proto/runtime/v1"
)

const (
	// StateConsistencyUndefined is the undefined value for state consistency.
	StateConsistencyUndefined StateConsistency = 0
	// StateConsistencyEventual represents eventual state consistency value.
	StateConsistencyEventual StateConsistency = 1
	// StateConsistencyStrong represents strong state consistency value.
	StateConsistencyStrong StateConsistency = 2

	// StateConcurrencyUndefined is the undefined value for state concurrency.
	StateConcurrencyUndefined StateConcurrency = 0
	// StateConcurrencyFirstWrite represents first write concurrency value.
	StateConcurrencyFirstWrite StateConcurrency = 1
	// StateConcurrencyLastWrite represents last write concurrency value.
	StateConcurrencyLastWrite StateConcurrency = 2

	// StateOperationTypeUndefined is the undefined value for state operation type.
	StateOperationTypeUndefined OperationType = 0
	// StateOperationTypeUpsert represents upsert operation type value.
	StateOperationTypeUpsert OperationType = 1
	// StateOperationTypeDelete represents delete operation type value.
	StateOperationTypeDelete OperationType = 2
	// UndefinedType represents undefined type value.
	UndefinedType = "undefined"
)

type (
	// StateConsistency is the consistency enum type.
	StateConsistency int
	// StateConcurrency is the concurrency enum type.
	StateConcurrency int
	// OperationType is the operation enum type.
	OperationType int
)

// GetPBConsistency get consistency pb value.
func (s StateConsistency) GetPBConsistency() v1.StateOptions_StateConsistency {
	return v1.StateOptions_StateConsistency(s)
}

// GetPBConcurrency get concurrency pb value.
func (s StateConcurrency) GetPBConcurrency() v1.StateOptions_StateConcurrency {
	return v1.StateOptions_StateConcurrency(s)
}

// String returns the string value of the OperationType.
func (o OperationType) String() string {
	names := [...]string{
		UndefinedType,
		"upsert",
		"delete",
	}
	if o < StateOperationTypeUpsert || o > StateOperationTypeDelete {
		return UndefinedType
	}

	return names[o]
}

// String returns the string value of the StateConsistency.
func (s StateConsistency) String() string {
	names := [...]string{
		UndefinedType,
		"strong",
		"eventual",
	}
	if s < StateConsistencyStrong || s > StateConsistencyEventual {
		return UndefinedType
	}

	return names[s]
}

// String returns the string value of the StateConcurrency.
func (s StateConcurrency) String() string {
	names := [...]string{
		UndefinedType,
		"first-write",
		"last-write",
	}
	if s < StateConcurrencyFirstWrite || s > StateConcurrencyLastWrite {
		return UndefinedType
	}

	return names[s]
}

var stateOptionDefault = &v1.StateOptions{
	Concurrency: v1.StateOptions_CONCURRENCY_LAST_WRITE,
	Consistency: v1.StateOptions_CONSISTENCY_STRONG,
}

// StateOperation is a collection of StateItems with a store name.
type StateOperation struct {
	Type OperationType
	Item *SetStateItem
}

// StateItem represents a single state item.
type StateItem struct {
	Key      string
	Value    []byte
	Etag     string
	Metadata map[string]string
}

// BulkStateItem represents a single state item.
type BulkStateItem struct {
	Key      string
	Value    []byte
	Etag     string
	Metadata map[string]string
	Error    string
}

// SetStateItem represents a single state to be persisted.
type SetStateItem struct {
	Key      string
	Value    []byte
	Etag     *ETag
	Metadata map[string]string
	Options  *StateOptions
}

// QueryItem represents a single query result item.
type QueryItem struct {
	Key   string
	Value []byte
	Etag  string
	Error string
}

// QueryResponse represents a query result.
type QueryResponse struct {
	Results  []QueryItem
	Token    string
	Metadata map[string]string
}

// DeleteStateItem represents a single state to be deleted.
type DeleteStateItem SetStateItem

// ETag represents an versioned record information.
type ETag struct {
	Value string
}

// StateOptions represents the state store persistence policy.
type StateOptions struct {
	Concurrency StateConcurrency
	Consistency StateConsistency
}

// StateOption StateOptions's function type.
type StateOption func(*StateOptions)

// WithConcurrency set StateOptions's Concurrency.
func WithConcurrency(concurrency StateConcurrency) StateOption {
	return func(so *StateOptions) {
		so.Concurrency = concurrency
	}
}

// WithConsistency set StateOptions's consistency.
func WithConsistency(consistency StateConsistency) StateOption {
	return func(so *StateOptions) {
		so.Consistency = consistency
	}
}

func toProtoSaveStateItem(si *SetStateItem) (item *v1.StateItem) {
	s := &v1.StateItem{
		Key:      si.Key,
		Metadata: si.Metadata,
		Value:    si.Value,
		Options:  toProtoStateOptions(si.Options),
	}

	if si.Etag != nil {
		s.Etag = &v1.Etag{
			Value: si.Etag.Value,
		}
	}

	return s
}

func toProtoStateOptions(so *StateOptions) (opts *v1.StateOptions) {
	if so == nil {
		return copyStateOptionDefaultPB()
	}
	return &v1.StateOptions{
		Concurrency: v1.StateOptions_StateConcurrency(so.Concurrency),
		Consistency: v1.StateOptions_StateConsistency(so.Consistency),
	}
}

func copyStateOptionDefaultPB() *v1.StateOptions {
	return &v1.StateOptions{
		Concurrency: stateOptionDefault.GetConcurrency(),
		Consistency: stateOptionDefault.GetConsistency(),
	}
}

func copyStateOptionDefault() *StateOptions {
	return &StateOptions{
		Concurrency: StateConcurrency(stateOptionDefault.GetConcurrency()),
		Consistency: StateConsistency(stateOptionDefault.GetConsistency()),
	}
}

func toProtoDuration(d time.Duration) *duration.Duration {
	nanos := d.Nanoseconds()
	secs := nanos / 1e9
	nanos -= secs * 1e9
	return &duration.Duration{
		Seconds: secs,
		Nanos:   int32(nanos),
	}
}

// ExecuteStateTransaction provides way to execute multiple operations on a specified store.
func (c *GRPCClient) ExecuteStateTransaction(ctx context.Context, storeName string, meta map[string]string, ops []*StateOperation) error {
	if storeName == "" {
		return errors.New("nil storeName")
	}
	if len(ops) == 0 {
		return nil
	}

	items := make([]*pb.TransactionalStateOperation, 0)
	for _, op := range ops {
		item := &pb.TransactionalStateOperation{
			OperationType: op.Type.String(),
			Request:       toProtoSaveStateItem(op.Item),
		}
		items = append(items, item)
	}

	req := &pb.ExecuteStateTransactionRequest{
		Metadata:   meta,
		StoreName:  storeName,
		Operations: items,
	}
	_, err := c.protoClient.ExecuteStateTransaction(c.withAuthToken(ctx), req)
	if err != nil {
		return fmt.Errorf("error executing state transaction: %w", err)
	}
	return nil
}

// SaveState saves the raw data into store, default options: strong, last-write.
func (c *GRPCClient) SaveState(ctx context.Context, storeName, key string, data []byte, meta map[string]string, so ...StateOption) error {
	return c.SaveStateWithETag(ctx, storeName, key, data, "", meta, so...)
}

// SaveStateWithETag saves the raw data into store using provided state options and etag.
func (c *GRPCClient) SaveStateWithETag(ctx context.Context, storeName, key string, data []byte, etag string, meta map[string]string, so ...StateOption) error {
	stateOptions := new(StateOptions)
	for _, o := range so {
		o(stateOptions)
	}
	if len(so) == 0 {
		stateOptions = copyStateOptionDefault()
	}
	item := &SetStateItem{
		Key:      key,
		Value:    data,
		Metadata: meta,
		Options:  stateOptions,
	}
	if etag != "" {
		item.Etag = &ETag{Value: etag}
	}
	return c.SaveBulkState(ctx, storeName, item)
}

// SaveBulkState saves the multiple state item to store.
func (c *GRPCClient) SaveBulkState(ctx context.Context, storeName string, items ...*SetStateItem) error {
	if storeName == "" {
		return errors.New("nil store")
	}
	if items == nil {
		return errors.New("nil item")
	}

	req := &pb.SaveStateRequest{
		StoreName: storeName,
		States:    make([]*v1.StateItem, 0),
	}

	for _, si := range items {
		item := toProtoSaveStateItem(si)
		req.States = append(req.States, item)
	}

	_, err := c.protoClient.SaveState(c.withAuthToken(ctx), req)
	if err != nil {
		return fmt.Errorf("error saving state: %w", err)
	}
	return nil
}

// GetBulkState retrieves state for multiple keys from specific store.
func (c *GRPCClient) GetBulkState(ctx context.Context, storeName string, keys []string, meta map[string]string, parallelism int32) ([]*BulkStateItem, error) {
	if storeName == "" {
		return nil, errors.New("nil store")
	}
	if len(keys) == 0 {
		return nil, errors.New("keys required")
	}
	items := make([]*BulkStateItem, 0)

	req := &pb.GetBulkStateRequest{
		StoreName:   storeName,
		Keys:        keys,
		Metadata:    meta,
		Parallelism: parallelism,
	}

	results, err := c.protoClient.GetBulkState(c.withAuthToken(ctx), req)
	if err != nil {
		return nil, fmt.Errorf("error getting state: %w", err)
	}

	if results == nil || results.Items == nil {
		return items, nil
	}

	for _, r := range results.Items {
		item := &BulkStateItem{
			Key:      r.Key,
			Etag:     r.Etag,
			Value:    r.Data,
			Metadata: r.Metadata,
			Error:    r.Error,
		}
		items = append(items, item)
	}

	return items, nil
}

// GetState retrieves state from specific store using default consistency option.
func (c *GRPCClient) GetState(ctx context.Context, storeName, key string, meta map[string]string) (item *StateItem, err error) {
	return c.GetStateWithConsistency(ctx, storeName, key, meta, StateConsistencyStrong)
}

// GetStateWithConsistency retrieves state from specific store using provided state consistency.
func (c *GRPCClient) GetStateWithConsistency(ctx context.Context, storeName, key string, meta map[string]string, sc StateConsistency) (*StateItem, error) {
	if err := hasRequiredStateArgs(storeName, key); err != nil {
		return nil, fmt.Errorf("missing required arguments: %w", err)
	}

	req := &pb.GetStateRequest{
		StoreName:   storeName,
		Key:         key,
		Consistency: v1.StateOptions_StateConsistency(sc),
		Metadata:    meta,
	}

	result, err := c.protoClient.GetState(c.withAuthToken(ctx), req)
	if err != nil {
		return nil, fmt.Errorf("error getting state: %w", err)
	}

	return &StateItem{
		Etag:     result.Etag,
		Key:      key,
		Value:    result.Data,
		Metadata: result.Metadata,
	}, nil
}

// QueryStateAlpha1 runs a query against state store.
func (c *GRPCClient) QueryStateAlpha1(ctx context.Context, storeName, query string, meta map[string]string) (*QueryResponse, error) {
	if storeName == "" {
		return nil, errors.New("store name is not set")
	}
	if query == "" {
		return nil, errors.New("query is not set")
	}
	req := &pb.QueryStateRequest{
		StoreName: storeName,
		Query:     query,
		Metadata:  meta,
	}
	resp, err := c.protoClient.QueryStateAlpha1(c.withAuthToken(ctx), req)
	if err != nil {
		return nil, fmt.Errorf("error querying state: %w", err)
	}

	ret := &QueryResponse{
		Results:  make([]QueryItem, len(resp.Results)),
		Token:    resp.Token,
		Metadata: resp.Metadata,
	}
	for i, item := range resp.Results {
		ret.Results[i].Key = item.Key
		ret.Results[i].Value = item.Data
		ret.Results[i].Etag = item.Etag
		ret.Results[i].Error = item.Error
	}

	return ret, nil
}

// DeleteState deletes content from store using default state options.
func (c *GRPCClient) DeleteState(ctx context.Context, storeName, key string, meta map[string]string) error {
	return c.DeleteStateWithETag(ctx, storeName, key, nil, meta, nil)
}

// DeleteStateWithETag deletes content from store using provided state options and etag.
func (c *GRPCClient) DeleteStateWithETag(ctx context.Context, storeName, key string, etag *ETag, meta map[string]string, opts *StateOptions) error {
	if err := hasRequiredStateArgs(storeName, key); err != nil {
		return fmt.Errorf("missing required arguments: %w", err)
	}

	req := &pb.DeleteStateRequest{
		StoreName: storeName,
		Key:       key,
		Options:   toProtoStateOptions(opts),
		Metadata:  meta,
	}

	if etag != nil {
		req.Etag = &v1.Etag{
			Value: etag.Value,
		}
	}

	_, err := c.protoClient.DeleteState(c.withAuthToken(ctx), req)
	if err != nil {
		return fmt.Errorf("error deleting state: %w", err)
	}

	return nil
}

// DeleteBulkState deletes content for multiple keys from store.
func (c *GRPCClient) DeleteBulkState(ctx context.Context, storeName string, keys []string, meta map[string]string) error {
	if len(keys) == 0 {
		return nil
	}

	items := make([]*DeleteStateItem, 0, len(keys))
	for i := 0; i < len(keys); i++ {
		item := &DeleteStateItem{
			Key:      keys[i],
			Metadata: meta,
		}
		items = append(items, item)
	}

	return c.DeleteBulkStateItems(ctx, storeName, items)
}

// DeleteBulkStateItems deletes content for multiple items from store.
func (c *GRPCClient) DeleteBulkStateItems(ctx context.Context, storeName string, items []*DeleteStateItem) error {
	if len(items) == 0 {
		return nil
	}

	states := make([]*v1.StateItem, 0, len(items))
	for i := 0; i < len(items); i++ {
		item := items[i]
		if err := hasRequiredStateArgs(storeName, item.Key); err != nil {
			return fmt.Errorf("missing required arguments: %w", err)
		}

		state := &v1.StateItem{
			Key:      item.Key,
			Metadata: item.Metadata,
			Options:  toProtoStateOptions(item.Options),
		}
		if item.Etag != nil {
			state.Etag = &v1.Etag{
				Value: item.Etag.Value,
			}
		}
		states = append(states, state)
	}

	req := &pb.DeleteBulkStateRequest{
		StoreName: storeName,
		States:    states,
	}
	_, err := c.protoClient.DeleteBulkState(c.withAuthToken(ctx), req)

	return err
}

func hasRequiredStateArgs(storeName, key string) error {
	if storeName == "" {
		return errors.New("store")
	}
	if key == "" {
		return errors.New("key")
	}
	return nil
}
