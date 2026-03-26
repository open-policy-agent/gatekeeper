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
	"time"

	"google.golang.org/grpc/connectivity"
)

// The following errors are returned from Wait.
var (
	// A call to Wait timed out while waiting for a gRPC connection to reach a Ready state.
	errWaitTimedOut = errors.New("timed out waiting for client connectivity")
)

func (c *GRPCClient) Wait(ctx context.Context, timeout time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// SDKs for other languages implement Wait by attempting to connect to a TCP endpoint
	// with a timeout. Go's SDKs handles more endpoints than just TCP ones. To simplify
	// the code here, we rely on GRPCs connectivity state management instead.
	// See https://github.com/grpc/grpc/blob/master/doc/connectivity-semantics-and-api.md
	for {
		curState := c.connection.GetState()
		if curState == connectivity.Ready {
			return nil
		}

		select {
		case <-timeoutCtx.Done():
			return errWaitTimedOut
		default:
			// Multiple state changes can happen: keep waiting for a successful one or time out
			c.connection.WaitForStateChange(timeoutCtx, curState)
		}
	}
}
