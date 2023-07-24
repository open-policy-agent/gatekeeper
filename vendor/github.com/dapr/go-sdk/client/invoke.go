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
	"errors"
	"fmt"
	"strings"

	anypb "github.com/golang/protobuf/ptypes/any"

	v1 "github.com/dapr/go-sdk/dapr/proto/common/v1"
	pb "github.com/dapr/go-sdk/dapr/proto/runtime/v1"
)

// DataContent the service invocation content.
type DataContent struct {
	// Data is the input data
	Data []byte
	// ContentType is the type of the data content
	ContentType string
}

func (c *GRPCClient) invokeServiceWithRequest(ctx context.Context, req *pb.InvokeServiceRequest) (out []byte, err error) {
	if req == nil {
		return nil, errors.New("nil request")
	}

	resp, err := c.protoClient.InvokeService(c.withAuthToken(ctx), req)
	if err != nil {
		return nil, err
	}

	// allow for service to not return any value
	if resp != nil && resp.GetData() != nil {
		out = resp.GetData().Value
		return
	}

	out = nil
	return
}

func queryAndVerbToHTTPExtension(query string, verb string) *v1.HTTPExtension {
	if v, ok := v1.HTTPExtension_Verb_value[strings.ToUpper(verb)]; ok {
		return &v1.HTTPExtension{Verb: v1.HTTPExtension_Verb(v), Querystring: query}
	}
	return &v1.HTTPExtension{Verb: v1.HTTPExtension_NONE}
}

func hasRequiredInvokeArgs(appID, methodName, verb string) error {
	if appID == "" {
		return errors.New("appID")
	}
	if methodName == "" {
		return errors.New("methodName")
	}
	if verb == "" {
		return errors.New("verb")
	}
	return nil
}

// InvokeMethod invokes service without raw data ([]byte).
func (c *GRPCClient) InvokeMethod(ctx context.Context, appID, methodName, verb string) (out []byte, err error) {
	if err := hasRequiredInvokeArgs(appID, methodName, verb); err != nil {
		return nil, fmt.Errorf("missing required parameter: %w", err)
	}
	method, query := extractMethodAndQuery(methodName)
	req := &pb.InvokeServiceRequest{
		Id: appID,
		Message: &v1.InvokeRequest{
			Method:        method,
			HttpExtension: queryAndVerbToHTTPExtension(query, verb),
		},
	}
	return c.invokeServiceWithRequest(ctx, req)
}

// InvokeMethodWithContent invokes service with content (data + content type).
func (c *GRPCClient) InvokeMethodWithContent(ctx context.Context, appID, methodName, verb string, content *DataContent) (out []byte, err error) {
	if err := hasRequiredInvokeArgs(appID, methodName, verb); err != nil {
		return nil, fmt.Errorf("missing required parameter: %w", err)
	}
	if content == nil {
		return nil, errors.New("content required")
	}
	method, query := extractMethodAndQuery(methodName)
	req := &pb.InvokeServiceRequest{
		Id: appID,
		Message: &v1.InvokeRequest{
			Method:        method,
			Data:          &anypb.Any{Value: content.Data},
			ContentType:   content.ContentType,
			HttpExtension: queryAndVerbToHTTPExtension(query, verb),
		},
	}
	return c.invokeServiceWithRequest(ctx, req)
}

// InvokeMethodWithCustomContent invokes service with custom content (struct + content type).
func (c *GRPCClient) InvokeMethodWithCustomContent(ctx context.Context, appID, methodName, verb string, contentType string, content interface{}) ([]byte, error) {
	if err := hasRequiredInvokeArgs(appID, methodName, verb); err != nil {
		return nil, fmt.Errorf("missing required parameter: %w", err)
	}
	if contentType == "" {
		return nil, errors.New("content type required")
	}
	if content == nil {
		return nil, errors.New("content required")
	}

	contentData, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("error serializing input struct: %w", err)
	}

	method, query := extractMethodAndQuery(methodName)

	req := &pb.InvokeServiceRequest{
		Id: appID,
		Message: &v1.InvokeRequest{
			Method:        method,
			Data:          &anypb.Any{Value: contentData},
			ContentType:   contentType,
			HttpExtension: queryAndVerbToHTTPExtension(query, verb),
		},
	}

	return c.invokeServiceWithRequest(ctx, req)
}

func extractMethodAndQuery(name string) (method, query string) {
	splitStr := strings.SplitN(name, "?", 2)
	method = splitStr[0]
	if len(splitStr) == 2 {
		query = splitStr[1]
	}
	return
}
