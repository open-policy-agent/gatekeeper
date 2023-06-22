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

// GetSecret retrieves preconfigured secret from specified store using key.
func (c *GRPCClient) GetSecret(ctx context.Context, storeName, key string, meta map[string]string) (data map[string]string, err error) {
	if storeName == "" {
		return nil, errors.New("empty storeName")
	}
	if key == "" {
		return nil, errors.New("empty key")
	}

	req := &pb.GetSecretRequest{
		Key:       key,
		StoreName: storeName,
		Metadata:  meta,
	}

	resp, err := c.protoClient.GetSecret(c.withAuthToken(ctx), req)
	if err != nil {
		return nil, fmt.Errorf("error invoking service: %w", err)
	}

	if resp != nil {
		data = resp.GetData()
	}

	return
}

// GetBulkSecret retrieves all preconfigured secrets for this application.
func (c *GRPCClient) GetBulkSecret(ctx context.Context, storeName string, meta map[string]string) (data map[string]map[string]string, err error) {
	if storeName == "" {
		return nil, errors.New("empty storeName")
	}

	req := &pb.GetBulkSecretRequest{
		StoreName: storeName,
		Metadata:  meta,
	}

	resp, err := c.protoClient.GetBulkSecret(c.withAuthToken(ctx), req)
	if err != nil {
		return nil, fmt.Errorf("error invoking service: %w", err)
	}

	if resp != nil {
		data = map[string]map[string]string{}

		for secretName, secretResponse := range resp.Data {
			data[secretName] = map[string]string{}

			for k, v := range secretResponse.Secrets {
				data[secretName][k] = v
			}
		}
	}

	return
}
