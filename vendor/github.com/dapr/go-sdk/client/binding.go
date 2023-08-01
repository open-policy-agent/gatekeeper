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

	pb "github.com/dapr/go-sdk/dapr/proto/runtime/v1"
)

// InvokeBindingRequest represents binding invocation request.
type InvokeBindingRequest struct {
	// Name is name of binding to invoke.
	Name string
	// Operation is the name of the operation type for the binding to invoke
	Operation string
	// Data is the input bindings sent
	Data []byte
	// Metadata is the input binding metadata
	Metadata map[string]string
}

// BindingEvent represents the binding event handler input.
type BindingEvent struct {
	// Data is the input bindings sent
	Data []byte
	// Metadata is the input binding metadata
	Metadata map[string]string
}

// InvokeBinding invokes specific operation on the configured Dapr binding.
// This method covers input, output, and bi-directional bindings.
func (c *GRPCClient) InvokeBinding(ctx context.Context, in *InvokeBindingRequest) (*BindingEvent, error) {
	if in == nil {
		return nil, errors.New("binding invocation required")
	}
	if in.Name == "" {
		return nil, errors.New("binding invocation name required")
	}
	if in.Operation == "" {
		return nil, errors.New("binding invocation operation required")
	}

	req := &pb.InvokeBindingRequest{
		Name:      in.Name,
		Operation: in.Operation,
		Data:      in.Data,
		Metadata:  in.Metadata,
	}

	resp, err := c.protoClient.InvokeBinding(c.withAuthToken(ctx), req)
	if err != nil {
		return nil, fmt.Errorf("error invoking binding %s/%s: %w", in.Name, in.Operation, err)
	}

	if resp != nil {
		return &BindingEvent{
			Data:     resp.Data,
			Metadata: resp.Metadata,
		}, nil
	}

	return nil, nil
}

// InvokeOutputBinding invokes configured Dapr binding with data (allows nil).InvokeOutputBinding
// This method differs from InvokeBinding in that it doesn't expect any content being returned from the invoked method.
func (c *GRPCClient) InvokeOutputBinding(ctx context.Context, in *InvokeBindingRequest) error {
	if _, err := c.InvokeBinding(ctx, in); err != nil {
		return fmt.Errorf("error invoking output binding: %w", err)
	}
	return nil
}
