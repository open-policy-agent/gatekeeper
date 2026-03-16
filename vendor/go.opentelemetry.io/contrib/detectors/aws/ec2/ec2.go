// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ec2 // import "go.opentelemetry.io/contrib/detectors/aws/ec2"

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

type config struct {
	c Client
}

// newConfig returns an appropriately configured config.
func newConfig(options ...Option) *config {
	c := new(config)
	for _, option := range options {
		option.apply(c)
	}

	return c
}

// Option applies an EC2 detector configuration option.
type Option interface {
	apply(*config)
}

type optionFunc func(*config)

func (fn optionFunc) apply(c *config) {
	fn(c)
}

// WithClient sets the ec2metadata client in config.
func WithClient(t Client) Option {
	return optionFunc(func(c *config) {
		c.c = t
	})
}

func (cfg *config) getClient() Client {
	return cfg.c
}

// resource detector collects resource information from EC2 environment.
type resourceDetector struct {
	c Client
}

// Client implements methods to capture EC2 environment metadata information.
type Client interface {
	Available() bool
	GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error)
	GetMetadata(p string) (string, error)
}

// compile time assertion that resourceDetector implements the resource.Detector interface.
var _ resource.Detector = (*resourceDetector)(nil)

// NewResourceDetector returns a resource detector that will detect AWS EC2 resources.
func NewResourceDetector(opts ...Option) resource.Detector {
	c := newConfig(opts...)
	return &resourceDetector{c.getClient()}
}

// Detect detects associated resources when running in AWS environment.
func (detector *resourceDetector) Detect(ctx context.Context) (*resource.Resource, error) {
	client, err := detector.client()
	if err != nil {
		return nil, err
	}

	if !client.Available() {
		return nil, nil
	}

	doc, err := client.GetInstanceIdentityDocument()
	if err != nil {
		return nil, err
	}

	attributes := []attribute.KeyValue{
		semconv.CloudProviderAWS,
		semconv.CloudPlatformAWSEC2,
		semconv.CloudRegion(doc.Region),
		semconv.CloudAvailabilityZone(doc.AvailabilityZone),
		semconv.CloudAccountID(doc.AccountID),
		semconv.HostID(doc.InstanceID),
		semconv.HostImageID(doc.ImageID),
		semconv.HostType(doc.InstanceType),
	}

	m := &metadata{client: client}
	m.add(semconv.HostNameKey, "hostname")

	attributes = append(attributes, m.attributes...)

	if len(m.errs) > 0 {
		err = fmt.Errorf("%w: %s", resource.ErrPartialResource, m.errs)
	}

	return resource.NewWithAttributes(semconv.SchemaURL, attributes...), err
}

func (detector *resourceDetector) client() (Client, error) {
	if detector.c != nil {
		return detector.c, nil
	}

	s, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	return ec2metadata.New(s), nil
}

type metadata struct {
	client     Client
	errs       []error
	attributes []attribute.KeyValue
}

func (m *metadata) add(k attribute.Key, n string) {
	v, err := m.client.GetMetadata(n)
	if err == nil {
		m.attributes = append(m.attributes, k.String(v))
		return
	}

	rf, ok := err.(awserr.RequestFailure)
	if !ok {
		m.errs = append(m.errs, fmt.Errorf("%q: %w", n, err))
		return
	}

	if rf.StatusCode() == http.StatusNotFound {
		return
	}

	m.errs = append(m.errs, fmt.Errorf("%q: %d %s", n, rf.StatusCode(), rf.Code()))
}
