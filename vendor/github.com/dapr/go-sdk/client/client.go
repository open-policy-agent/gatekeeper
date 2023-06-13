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
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/dapr/go-sdk/actor"
	"github.com/dapr/go-sdk/actor/config"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/dapr/go-sdk/dapr/proto/runtime/v1"

	// used to import codec implements.
	_ "github.com/dapr/go-sdk/actor/codec/impl"
)

const (
	daprPortDefault               = "50001"
	daprPortEnvVarName            = "DAPR_GRPC_PORT" /* #nosec */
	traceparentKey                = "traceparent"
	apiTokenKey                   = "dapr-api-token" /* #nosec */
	apiTokenEnvVarName            = "DAPR_API_TOKEN" /* #nosec */
	clientDefaultTimoutSeconds    = 5
	clientTimoutSecondsEnvVarName = "DAPR_CLIENT_TIMEOUT_SECONDS"
)

var (
	logger               = log.New(os.Stdout, "", 0)
	_             Client = (*GRPCClient)(nil)
	defaultClient Client
	doOnce        sync.Once
)

// Client is the interface for Dapr client implementation.
type Client interface {
	// InvokeBinding invokes specific operation on the configured Dapr binding.
	// This method covers input, output, and bi-directional bindings.
	InvokeBinding(ctx context.Context, in *InvokeBindingRequest) (out *BindingEvent, err error)

	// InvokeOutputBinding invokes configured Dapr binding with data.InvokeOutputBinding
	// This method differs from InvokeBinding in that it doesn't expect any content being returned from the invoked method.
	InvokeOutputBinding(ctx context.Context, in *InvokeBindingRequest) error

	// InvokeMethod invokes service without raw data
	InvokeMethod(ctx context.Context, appID, methodName, verb string) (out []byte, err error)

	// InvokeMethodWithContent invokes service with content
	InvokeMethodWithContent(ctx context.Context, appID, methodName, verb string, content *DataContent) (out []byte, err error)

	// InvokeMethodWithCustomContent invokes app with custom content (struct + content type).
	InvokeMethodWithCustomContent(ctx context.Context, appID, methodName, verb string, contentType string, content interface{}) (out []byte, err error)

	// PublishEvent publishes data onto topic in specific pubsub component.
	PublishEvent(ctx context.Context, pubsubName, topicName string, data interface{}, opts ...PublishEventOption) error

	// PublishEventfromCustomContent serializes an struct and publishes its contents as data (JSON) onto topic in specific pubsub component.
	// Deprecated: This method is deprecated and will be removed in a future version of the SDK. Please use `PublishEvent` instead.
	PublishEventfromCustomContent(ctx context.Context, pubsubName, topicName string, data interface{}) error

	// GetSecret retrieves preconfigured secret from specified store using key.
	GetSecret(ctx context.Context, storeName, key string, meta map[string]string) (data map[string]string, err error)

	// GetBulkSecret retrieves all preconfigured secrets for this application.
	GetBulkSecret(ctx context.Context, storeName string, meta map[string]string) (data map[string]map[string]string, err error)

	// SaveState saves the raw data into store using default state options.
	SaveState(ctx context.Context, storeName, key string, data []byte, meta map[string]string, so ...StateOption) error

	// SaveState saves the raw data into store using provided state options and etag.
	SaveStateWithETag(ctx context.Context, storeName, key string, data []byte, etag string, meta map[string]string, so ...StateOption) error

	// SaveBulkState saves multiple state item to store with specified options.
	SaveBulkState(ctx context.Context, storeName string, items ...*SetStateItem) error

	// GetState retrieves state from specific store using default consistency option.
	GetState(ctx context.Context, storeName, key string, meta map[string]string) (item *StateItem, err error)

	// GetStateWithConsistency retrieves state from specific store using provided state consistency.
	GetStateWithConsistency(ctx context.Context, storeName, key string, meta map[string]string, sc StateConsistency) (item *StateItem, err error)

	// GetBulkState retrieves state for multiple keys from specific store.
	GetBulkState(ctx context.Context, storeName string, keys []string, meta map[string]string, parallelism int32) ([]*BulkStateItem, error)

	// QueryStateAlpha1 runs a query against state store.
	QueryStateAlpha1(ctx context.Context, storeName, query string, meta map[string]string) (*QueryResponse, error)

	// DeleteState deletes content from store using default state options.
	DeleteState(ctx context.Context, storeName, key string, meta map[string]string) error

	// DeleteStateWithETag deletes content from store using provided state options and etag.
	DeleteStateWithETag(ctx context.Context, storeName, key string, etag *ETag, meta map[string]string, opts *StateOptions) error

	// ExecuteStateTransaction provides way to execute multiple operations on a specified store.
	ExecuteStateTransaction(ctx context.Context, storeName string, meta map[string]string, ops []*StateOperation) error

	// GetConfigurationItem can get target configuration item by storeName and key
	GetConfigurationItem(ctx context.Context, storeName, key string, opts ...ConfigurationOpt) (*ConfigurationItem, error)

	// GetConfigurationItems can get a list of configuration item by storeName and keys
	GetConfigurationItems(ctx context.Context, storeName string, keys []string, opts ...ConfigurationOpt) (map[string]*ConfigurationItem, error)

	// SubscribeConfigurationItems can subscribe the change of configuration items by storeName and keys, and return subscription id
	SubscribeConfigurationItems(ctx context.Context, storeName string, keys []string, handler ConfigurationHandleFunction, opts ...ConfigurationOpt) error

	// UnsubscribeConfigurationItems can stop the subscription with target store's and id
	UnsubscribeConfigurationItems(ctx context.Context, storeName string, id string, opts ...ConfigurationOpt) error

	// DeleteBulkState deletes content for multiple keys from store.
	DeleteBulkState(ctx context.Context, storeName string, keys []string, meta map[string]string) error

	// DeleteBulkStateItems deletes content for multiple items from store.
	DeleteBulkStateItems(ctx context.Context, storeName string, items []*DeleteStateItem) error

	// TryLockAlpha1 attempts to grab a lock from a lock store.
	TryLockAlpha1(ctx context.Context, storeName string, request *LockRequest) (*LockResponse, error)

	// UnlockAlpha1 deletes unlocks a lock from a lock store.
	UnlockAlpha1(ctx context.Context, storeName string, request *UnlockRequest) (*UnlockResponse, error)

	// Shutdown the sidecar.
	Shutdown(ctx context.Context) error

	// WithTraceID adds existing trace ID to the outgoing context.
	WithTraceID(ctx context.Context, id string) context.Context

	// WithAuthToken sets Dapr API token on the instantiated client.
	WithAuthToken(token string)

	// Close cleans up all resources created by the client.
	Close()

	// RegisterActorTimer registers an actor timer.
	RegisterActorTimer(ctx context.Context, req *RegisterActorTimerRequest) error

	// UnregisterActorTimer unregisters an actor timer.
	UnregisterActorTimer(ctx context.Context, req *UnregisterActorTimerRequest) error

	// RegisterActorReminder registers an actor reminder.
	RegisterActorReminder(ctx context.Context, req *RegisterActorReminderRequest) error

	// UnregisterActorReminder unregisters an actor reminder.
	UnregisterActorReminder(ctx context.Context, req *UnregisterActorReminderRequest) error

	// RenameActorReminder rename an actor reminder.
	RenameActorReminder(ctx context.Context, req *RenameActorReminderRequest) error

	// InvokeActor calls a method on an actor.
	InvokeActor(ctx context.Context, req *InvokeActorRequest) (*InvokeActorResponse, error)

	// GetActorState get actor state
	GetActorState(ctx context.Context, req *GetActorStateRequest) (data *GetActorStateResponse, err error)

	// SaveStateTransactionally save actor state
	SaveStateTransactionally(ctx context.Context, actorType, actorID string, operations []*ActorStateOperation) error

	// ImplActorClientStub is to impl user defined actor client stub
	ImplActorClientStub(actorClientStub actor.Client, opt ...config.Option)

	// GrpcClient returns the base grpc client if grpc is used and nil otherwise
	GrpcClient() pb.DaprClient
}

// NewClient instantiates Dapr client using DAPR_GRPC_PORT environment variable as port.
// Note, this default factory function creates Dapr client only once. All subsequent invocations
// will return the already created instance. To create multiple instances of the Dapr client,
// use one of the parameterized factory functions:
//
//	NewClientWithPort(port string) (client Client, err error)
//	NewClientWithAddress(address string) (client Client, err error)
//	NewClientWithConnection(conn *grpc.ClientConn) Client
//	NewClientWithSocket(socket string) (client Client, err error)
func NewClient() (client Client, err error) {
	port := os.Getenv(daprPortEnvVarName)
	if port == "" {
		port = daprPortDefault
	}
	var onceErr error
	doOnce.Do(func() {
		c, err := NewClientWithPort(port)
		onceErr = errors.Wrap(err, "error creating default client")
		defaultClient = c
	})

	return defaultClient, onceErr
}

// NewClientWithPort instantiates Dapr using specific gRPC port.
func NewClientWithPort(port string) (client Client, err error) {
	if port == "" {
		return nil, errors.New("nil port")
	}
	return NewClientWithAddress(net.JoinHostPort("127.0.0.1", port))
}

// NewClientWithAddress instantiates Dapr using specific address (including port).
func NewClientWithAddress(address string) (client Client, err error) {
	if address == "" {
		return nil, errors.New("nil address")
	}
	logger.Printf("dapr client initializing for: %s", address)

	timeoutSeconds, err := getClientTimeoutSeconds()
	if err != nil {
		return nil, err
	}
	ctx, ctxCancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	conn, err := grpc.DialContext(
		ctx,
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		ctxCancel()
		return nil, errors.Wrapf(err, "error creating connection to '%s': %v", address, err)
	}
	if hasToken := os.Getenv(apiTokenEnvVarName); hasToken != "" {
		logger.Println("client uses API token")
	}

	return newClientWithConnectionAndCancelFunc(conn, ctxCancel), nil
}

func getClientTimeoutSeconds() (int, error) {
	timeoutStr := os.Getenv(clientTimoutSecondsEnvVarName)
	if len(timeoutStr) == 0 {
		return clientDefaultTimoutSeconds, nil
	}
	timeoutVar, err := strconv.Atoi(timeoutStr)
	if err != nil {
		return 0, err
	}
	if timeoutVar <= 0 {
		return 0, errors.New("incorrect value")
	}
	return timeoutVar, nil
}

// NewClientWithSocket instantiates Dapr using specific socket.
func NewClientWithSocket(socket string) (client Client, err error) {
	if socket == "" {
		return nil, errors.New("nil socket")
	}
	logger.Printf("dapr client initializing for: %s", socket)
	addr := fmt.Sprintf("unix://%s", socket)
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, errors.Wrapf(err, "error creating connection to '%s': %v", addr, err)
	}
	if hasToken := os.Getenv(apiTokenEnvVarName); hasToken != "" {
		logger.Println("client uses API token")
	}
	return NewClientWithConnection(conn), nil
}

// NewClientWithConnection instantiates Dapr client using specific connection.
func NewClientWithConnection(conn *grpc.ClientConn) Client {
	return newClientWithConnectionAndCancelFunc(conn, func() {})
}

func newClientWithConnectionAndCancelFunc(
	conn *grpc.ClientConn,
	cancelFunc context.CancelFunc,
) Client {
	return &GRPCClient{
		connection:    conn,
		ctxCancelFunc: cancelFunc,
		protoClient:   pb.NewDaprClient(conn),
		authToken:     os.Getenv(apiTokenEnvVarName),
	}
}

// GRPCClient is the gRPC implementation of Dapr client.
type GRPCClient struct {
	connection    *grpc.ClientConn
	ctxCancelFunc context.CancelFunc
	protoClient   pb.DaprClient
	authToken     string
}

// Close cleans up all resources created by the client.
func (c *GRPCClient) Close() {
	c.ctxCancelFunc()
	if c.connection != nil {
		c.connection.Close()
		c.connection = nil
	}
}

// WithAuthToken sets Dapr API token on the instantiated client.
// Allows empty string to reset token on existing client.
func (c *GRPCClient) WithAuthToken(token string) {
	c.authToken = token
}

// WithTraceID adds existing trace ID to the outgoing context.
func (c *GRPCClient) WithTraceID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	logger.Printf("using trace parent ID: %s", id)
	md := metadata.Pairs(traceparentKey, id)
	return metadata.NewOutgoingContext(ctx, md)
}

func (c *GRPCClient) withAuthToken(ctx context.Context) context.Context {
	if c.authToken == "" {
		return ctx
	}
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(apiTokenKey, c.authToken))
}

// Shutdown the sidecar.
func (c *GRPCClient) Shutdown(ctx context.Context) error {
	_, err := c.protoClient.Shutdown(c.withAuthToken(ctx), &emptypb.Empty{})
	if err != nil {
		return errors.Wrap(err, "error shutting down the sidecar")
	}
	return nil
}

// GrpcClient returns the base grpc client.
func (c *GRPCClient) GrpcClient() pb.DaprClient {
	return c.protoClient
}

// GrpcClientConn returns the grpc.ClientConn object used by this client.
func (c *GRPCClient) GrpcClientConn() *grpc.ClientConn {
	return c.connection
}
