package client

import (
	"context"
	"fmt"
	"io"

	"github.com/pkg/errors"

	pb "github.com/dapr/go-sdk/dapr/proto/runtime/v1"
)

type ConfigurationItem struct {
	Value    string
	Version  string
	Metadata map[string]string
}

type ConfigurationOpt func(map[string]string)

func WithConfigurationMetadata(key, value string) ConfigurationOpt {
	return func(m map[string]string) {
		m[key] = value
	}
}

func (c *GRPCClient) GetConfigurationItem(ctx context.Context, storeName, key string, opts ...ConfigurationOpt) (*ConfigurationItem, error) {
	items, err := c.GetConfigurationItems(ctx, storeName, []string{key}, opts...)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}

	return items[key], nil
}

func (c *GRPCClient) GetConfigurationItems(ctx context.Context, storeName string, keys []string, opts ...ConfigurationOpt) (map[string]*ConfigurationItem, error) {
	metadata := make(map[string]string)
	for _, opt := range opts {
		opt(metadata)
	}
	rsp, err := c.protoClient.GetConfigurationAlpha1(ctx, &pb.GetConfigurationRequest{
		StoreName: storeName,
		Keys:      keys,
		Metadata:  metadata,
	})
	if err != nil {
		return nil, err
	}

	configItems := make(map[string]*ConfigurationItem)
	for k, v := range rsp.Items {
		configItems[k] = &ConfigurationItem{
			Value:    v.Value,
			Version:  v.Version,
			Metadata: v.Metadata,
		}
	}
	return configItems, nil
}

type ConfigurationHandleFunction func(string, map[string]*ConfigurationItem)

func (c *GRPCClient) SubscribeConfigurationItems(ctx context.Context, storeName string, keys []string, handler ConfigurationHandleFunction, opts ...ConfigurationOpt) error {
	metadata := make(map[string]string)
	for _, opt := range opts {
		opt(metadata)
	}

	client, err := c.protoClient.SubscribeConfigurationAlpha1(ctx, &pb.SubscribeConfigurationRequest{
		StoreName: storeName,
		Keys:      keys,
		Metadata:  metadata,
	})
	if err != nil {
		return errors.Errorf("subscribe configuration failed with error = %s", err)
	}

	var subscribeID string
	stopCh := make(chan struct{})
	go func() {
		for {
			rsp, err := client.Recv()
			if err == io.EOF || rsp == nil {
				// receive goroutine would close if unsubscribe is called
				fmt.Println("dapr configuration subscribe finished.")
				close(stopCh)
				break
			}
			subscribeID = rsp.Id
			configurationItems := make(map[string]*ConfigurationItem)
			for k, v := range rsp.Items {
				configurationItems[k] = &ConfigurationItem{
					Value:    v.Value,
					Version:  v.Version,
					Metadata: v.Metadata,
				}
			}
			handler(rsp.Id, configurationItems)
		}
	}()
	select {
	case <-ctx.Done():
		return c.UnsubscribeConfigurationItems(context.Background(), storeName, subscribeID)
	case <-stopCh:
		return nil
	}
}

func (c *GRPCClient) UnsubscribeConfigurationItems(ctx context.Context, storeName string, id string, opts ...ConfigurationOpt) error {
	alpha1, err := c.protoClient.UnsubscribeConfigurationAlpha1(ctx, &pb.UnsubscribeConfigurationRequest{
		StoreName: storeName,
		Id:        id,
	})
	if err != nil {
		return err
	}
	if !alpha1.Ok {
		return errors.Errorf("unsubscribe error message = %s", alpha1.GetMessage())
	}
	return nil
}
