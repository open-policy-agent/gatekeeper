package client

import (
	"context"
	"errors"
	"fmt"
	"io"

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
	rsp, err := c.protoClient.GetConfiguration(ctx, &pb.GetConfigurationRequest{
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

func (c *GRPCClient) SubscribeConfigurationItems(ctx context.Context, storeName string, keys []string, handler ConfigurationHandleFunction, opts ...ConfigurationOpt) (string, error) {
	metadata := make(map[string]string)
	for _, opt := range opts {
		opt(metadata)
	}

	client, err := c.protoClient.SubscribeConfiguration(ctx, &pb.SubscribeConfigurationRequest{
		StoreName: storeName,
		Keys:      keys,
		Metadata:  metadata,
	})
	if err != nil {
		return "", fmt.Errorf("subscribe configuration failed with error = %w", err)
	}
	subscribeIDChan := make(chan string, 1)
	go func() {
		isFirst := true
		for {
			rsp, err := client.Recv()
			if errors.Is(err, io.EOF) || rsp == nil {
				// receive goroutine would close if unsubscribe is called.
				fmt.Println("dapr configuration subscribe finished.")
				break
			}
			configurationItems := make(map[string]*ConfigurationItem)

			for k, v := range rsp.Items {
				configurationItems[k] = &ConfigurationItem{
					Value:    v.Value,
					Version:  v.Version,
					Metadata: v.Metadata,
				}
			}
			// Get the subscription ID from the first response.
			if isFirst {
				subscribeIDChan <- rsp.Id
				isFirst = false
			}
			// Do not invoke handler in case there are no items.
			if len(configurationItems) > 0 {
				handler(rsp.Id, configurationItems)
			}
		}
	}()
	subscribeID := <-subscribeIDChan
	close(subscribeIDChan)
	return subscribeID, nil
}

func (c *GRPCClient) UnsubscribeConfigurationItems(ctx context.Context, storeName string, id string, opts ...ConfigurationOpt) error {
	resp, err := c.protoClient.UnsubscribeConfiguration(ctx, &pb.UnsubscribeConfigurationRequest{
		StoreName: storeName,
		Id:        id,
	})
	if err != nil {
		return fmt.Errorf("unsubscribe failed with error = %w", err)
	}
	if !resp.Ok {
		return fmt.Errorf("unsubscribe error message = %s", resp.GetMessage())
	}
	return nil
}
