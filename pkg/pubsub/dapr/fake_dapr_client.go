package dapr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	daprClient "github.com/dapr/go-sdk/client"
	commonv1pb "github.com/dapr/go-sdk/dapr/proto/common/v1"
	pb "github.com/dapr/go-sdk/dapr/proto/runtime/v1"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/google/uuid"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/pubsub/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	testBufSize = 1024 * 1024
	testSocket  = "/tmp/dapr.socket"
	valueSuffix = "_value"
)

var logger = log.New(os.Stdout, "", 0)

func getTestClient(ctx context.Context) (client daprClient.Client, closer func()) {
	s := grpc.NewServer()
	pb.RegisterDaprServer(s, &testDaprServer{
		state:                       make(map[string][]byte),
		configurationSubscriptionID: map[string]chan struct{}{},
	})

	l := bufconn.Listen(testBufSize)
	go func() {
		if err := s.Serve(l); err != nil && err.Error() != "closed" {
			logger.Fatalf("test server exited with error: %v", err)
		}
	}()

	d := grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return l.Dial()
	})

	c, err := grpc.DialContext(ctx, "", d, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Fatalf("failed to dial test context: %v", err)
	}

	closer = func() {
		l.Close()
		s.Stop()
	}

	client = daprClient.NewClientWithConnection(c)
	return
}

type testDaprServer struct {
	pb.UnimplementedDaprServer
	state                             map[string][]byte
	configurationSubscriptionIDMapLoc sync.Mutex
	configurationSubscriptionID       map[string]chan struct{}
}

func (s *testDaprServer) TryLockAlpha1(ctx context.Context, req *pb.TryLockRequest) (*pb.TryLockResponse, error) {
	return &pb.TryLockResponse{
		Success: true,
	}, nil
}

func (s *testDaprServer) UnlockAlpha1(ctx context.Context, req *pb.UnlockRequest) (*pb.UnlockResponse, error) {
	return &pb.UnlockResponse{
		Status: pb.UnlockResponse_SUCCESS,
	}, nil
}

func (s *testDaprServer) InvokeService(ctx context.Context, req *pb.InvokeServiceRequest) (*commonv1pb.InvokeResponse, error) {
	if req.Message == nil {
		return &commonv1pb.InvokeResponse{
			ContentType: "text/plain",
			Data: &anypb.Any{
				Value: []byte("pong"),
			},
		}, nil
	}
	return &commonv1pb.InvokeResponse{
		ContentType: req.Message.ContentType,
		Data:        req.Message.Data,
	}, nil
}

func (s *testDaprServer) GetState(ctx context.Context, req *pb.GetStateRequest) (*pb.GetStateResponse, error) {
	return &pb.GetStateResponse{
		Data: s.state[req.Key],
		Etag: "1",
	}, nil
}

func (s *testDaprServer) GetBulkState(ctx context.Context, in *pb.GetBulkStateRequest) (*pb.GetBulkStateResponse, error) {
	items := make([]*pb.BulkStateItem, 0)
	for _, k := range in.GetKeys() {
		if v, found := s.state[k]; found {
			item := &pb.BulkStateItem{
				Key:  k,
				Etag: "1",
				Data: v,
			}
			items = append(items, item)
		}
	}
	return &pb.GetBulkStateResponse{
		Items: items,
	}, nil
}

func (s *testDaprServer) SaveState(ctx context.Context, req *pb.SaveStateRequest) (*empty.Empty, error) {
	for _, item := range req.States {
		s.state[item.Key] = item.Value
	}
	return &empty.Empty{}, nil
}

func (s *testDaprServer) QueryStateAlpha1(ctx context.Context, req *pb.QueryStateRequest) (*pb.QueryStateResponse, error) {
	var v map[string]interface{}
	if err := json.Unmarshal([]byte(req.Query), &v); err != nil {
		return nil, err
	}

	ret := &pb.QueryStateResponse{
		Results: make([]*pb.QueryStateItem, 0, len(s.state)),
	}
	for key, value := range s.state {
		ret.Results = append(ret.Results, &pb.QueryStateItem{Key: key, Data: value})
	}
	return ret, nil
}

func (s *testDaprServer) DeleteState(ctx context.Context, req *pb.DeleteStateRequest) (*empty.Empty, error) {
	delete(s.state, req.Key)
	return &empty.Empty{}, nil
}

func (s *testDaprServer) DeleteBulkState(ctx context.Context, req *pb.DeleteBulkStateRequest) (*empty.Empty, error) {
	for _, item := range req.States {
		delete(s.state, item.Key)
	}
	return &empty.Empty{}, nil
}

func (s *testDaprServer) ExecuteStateTransaction(ctx context.Context, in *pb.ExecuteStateTransactionRequest) (*empty.Empty, error) {
	for _, op := range in.GetOperations() {
		item := op.GetRequest()
		switch opType := op.GetOperationType(); opType {
		case "upsert":
			s.state[item.Key] = item.Value
		case "delete":
			delete(s.state, item.Key)
		default:
			return &empty.Empty{}, fmt.Errorf("invalid operation type: %s", opType)
		}
	}
	return &empty.Empty{}, nil
}

func (s *testDaprServer) PublishEvent(ctx context.Context, req *pb.PublishEventRequest) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (s *testDaprServer) InvokeBinding(ctx context.Context, req *pb.InvokeBindingRequest) (*pb.InvokeBindingResponse, error) {
	if req.Data == nil {
		return &pb.InvokeBindingResponse{
			Data:     []byte("test"),
			Metadata: map[string]string{"k1": "v1", "k2": "v2"},
		}, nil
	}
	return &pb.InvokeBindingResponse{
		Data:     req.Data,
		Metadata: req.Metadata,
	}, nil
}

func (s *testDaprServer) GetSecret(ctx context.Context, req *pb.GetSecretRequest) (*pb.GetSecretResponse, error) {
	d := make(map[string]string)
	d["test"] = "value"
	return &pb.GetSecretResponse{
		Data: d,
	}, nil
}

func (s *testDaprServer) GetBulkSecret(ctx context.Context, req *pb.GetBulkSecretRequest) (*pb.GetBulkSecretResponse, error) {
	d := make(map[string]*pb.SecretResponse)
	d["test"] = &pb.SecretResponse{
		Secrets: map[string]string{
			"test": "value",
		},
	}
	return &pb.GetBulkSecretResponse{
		Data: d,
	}, nil
}

func (s *testDaprServer) RegisterActorReminder(ctx context.Context, req *pb.RegisterActorReminderRequest) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (s *testDaprServer) UnregisterActorReminder(ctx context.Context, req *pb.UnregisterActorReminderRequest) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (s *testDaprServer) RenameActorReminder(ctx context.Context, req *pb.RenameActorReminderRequest) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (s *testDaprServer) InvokeActor(context.Context, *pb.InvokeActorRequest) (*pb.InvokeActorResponse, error) {
	return &pb.InvokeActorResponse{
		Data: []byte("mockValue"),
	}, nil
}

func (s *testDaprServer) RegisterActorTimer(context.Context, *pb.RegisterActorTimerRequest) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (s *testDaprServer) UnregisterActorTimer(context.Context, *pb.UnregisterActorTimerRequest) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (s *testDaprServer) Shutdown(ctx context.Context, req *empty.Empty) (*empty.Empty, error) {
	return &empty.Empty{}, nil
}

func (s *testDaprServer) GetConfiguration(ctx context.Context, in *pb.GetConfigurationRequest) (*pb.GetConfigurationResponse, error) {
	if in.GetStoreName() == "" {
		return &pb.GetConfigurationResponse{}, errors.New("store name notfound")
	}
	items := make(map[string]*commonv1pb.ConfigurationItem)
	for _, v := range in.GetKeys() {
		items[v] = &commonv1pb.ConfigurationItem{
			Value: v + valueSuffix,
		}
	}
	return &pb.GetConfigurationResponse{
		Items: items,
	}, nil
}

func (s *testDaprServer) SubscribeConfiguration(in *pb.SubscribeConfigurationRequest, server pb.Dapr_SubscribeConfigurationServer) error {
	stopCh := make(chan struct{})
	id, _ := uuid.NewUUID()
	s.configurationSubscriptionIDMapLoc.Lock()
	s.configurationSubscriptionID[id.String()] = stopCh
	s.configurationSubscriptionIDMapLoc.Unlock()

	// Send subscription ID in the first response.
	if err := server.Send(&pb.SubscribeConfigurationResponse{
		Id: id.String(),
	}); err != nil {
		return err
	}

	for i := 0; i < 5; i++ {
		select {
		case <-stopCh:
			return nil
		default:
		}
		items := make(map[string]*commonv1pb.ConfigurationItem)
		for _, v := range in.GetKeys() {
			items[v] = &commonv1pb.ConfigurationItem{
				Value: v + valueSuffix,
			}
		}
		if err := server.Send(&pb.SubscribeConfigurationResponse{
			Id:    id.String(),
			Items: items,
		}); err != nil {
			return err
		}
		time.Sleep(time.Second)
	}
	return nil
}

func (s *testDaprServer) UnsubscribeConfiguration(ctx context.Context, in *pb.UnsubscribeConfigurationRequest) (*pb.UnsubscribeConfigurationResponse, error) {
	s.configurationSubscriptionIDMapLoc.Lock()
	defer s.configurationSubscriptionIDMapLoc.Unlock()
	ch, ok := s.configurationSubscriptionID[in.Id]
	if !ok {
		return &pb.UnsubscribeConfigurationResponse{Ok: true}, nil
	}
	close(ch)
	delete(s.configurationSubscriptionID, in.Id)
	return &pb.UnsubscribeConfigurationResponse{Ok: true}, nil
}

// BulkPublishEventAlpha1 mocks the BulkPublishEventAlpha1 API.
// It will fail to publish events that start with "fail".
// It will fail the entire request if an event starts with "failall".
func (s *testDaprServer) BulkPublishEventAlpha1(ctx context.Context, req *pb.BulkPublishRequest) (*pb.BulkPublishResponse, error) {
	failedEntries := make([]*pb.BulkPublishResponseFailedEntry, 0)
	for _, entry := range req.Entries {
		if bytes.HasPrefix(entry.Event, []byte("failall")) {
			// fail the entire request
			return nil, errors.New("failed to publish events")
		} else if bytes.HasPrefix(entry.Event, []byte("fail")) {
			// fail this entry
			failedEntries = append(failedEntries, &pb.BulkPublishResponseFailedEntry{
				EntryId: entry.EntryId,
				Error:   "failed to publish events",
			})
		}
	}
	return &pb.BulkPublishResponse{FailedEntries: failedEntries}, nil
}

func FakeConnection() (connection.Connection, func()) {
	ctx := context.Background()
	c, f := getTestClient(ctx)
	return &Dapr{
		client:          c,
		pubSubComponent: "test",
	}, f
}

type FakeDapr struct {
	// Array of clients to talk to different endpoints
	client daprClient.Client

	// Name of the pubsub component
	pubSubComponent string

	// closing function
	f func()
}

func (r *FakeDapr) Publish(ctx context.Context, data interface{}, topic string) error {
	return nil
}

func (r *FakeDapr) CloseConnection() error {
	r.f()
	return nil
}

func (r *FakeDapr) UpdateConnection(_ context.Context, config interface{}) error {
	var cfg ClientConfig
	m, ok := config.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid type assertion, config is not in expected format")
	}
	cfg.Component, ok = m["component"].(string)
	if !ok {
		return fmt.Errorf("failed to get value of component")
	}
	r.pubSubComponent = cfg.Component
	return nil
}

// Returns a fake client for dapr.
func FakeNewConnection(ctx context.Context, config interface{}) (connection.Connection, error) {
	var cfg ClientConfig
	m, ok := config.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid type assertion, config is not in expected format")
	}
	cfg.Component, ok = m["component"].(string)
	if !ok {
		return nil, fmt.Errorf("failed to get value of component")
	}

	c, f := getTestClient(ctx)

	return &FakeDapr{
		client:          c,
		pubSubComponent: cfg.Component,
		f:               f,
	}, nil
}
