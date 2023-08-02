/*
Copyright 2022 The Dapr Authors
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

// LockRequest is the lock request object.
type LockRequest struct {
	ResourceID      string
	LockOwner       string
	ExpiryInSeconds int32
}

// UnlockRequest is the unlock request object.
type UnlockRequest struct {
	ResourceID string
	LockOwner  string
}

// LockResponse is the lock operation response object.
type LockResponse struct {
	Success bool
}

// UnlockResponse is the unlock operation response object.
type UnlockResponse struct {
	StatusCode int32
	Status     string
}

// TryLockAlpha1 attempts to grab a lock from a lock store.
func (c *GRPCClient) TryLockAlpha1(ctx context.Context, storeName string, request *LockRequest) (*LockResponse, error) {
	if storeName == "" {
		return nil, errors.New("storeName is empty")
	}

	if request == nil {
		return nil, errors.New("request is nil")
	}

	req := pb.TryLockRequest{
		ResourceId:      request.ResourceID,
		LockOwner:       request.LockOwner,
		ExpiryInSeconds: request.ExpiryInSeconds,
		StoreName:       storeName,
	}

	resp, err := c.protoClient.TryLockAlpha1(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("error getting lock: %w", err)
	}

	return &LockResponse{
		Success: resp.Success,
	}, nil
}

// UnlockAlpha1 deletes unlocks a lock from a lock store.
func (c *GRPCClient) UnlockAlpha1(ctx context.Context, storeName string, request *UnlockRequest) (*UnlockResponse, error) {
	if storeName == "" {
		return nil, errors.New("storeName is empty")
	}

	if request == nil {
		return nil, errors.New("request is nil")
	}

	req := pb.UnlockRequest{
		ResourceId: request.ResourceID,
		LockOwner:  request.LockOwner,
		StoreName:  storeName,
	}

	resp, err := c.protoClient.UnlockAlpha1(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("error getting lock: %w", err)
	}

	return &UnlockResponse{
		StatusCode: int32(resp.Status),
		Status:     pb.UnlockResponse_Status_name[int32(resp.Status)],
	}, nil
}
