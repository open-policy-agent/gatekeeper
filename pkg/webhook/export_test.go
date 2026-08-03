package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	"github.com/stretchr/testify/require"
)

type fakeAdmissionExportSystem struct {
	publishErr error
	published  chan interface{}
	publish    func(context.Context, interface{}) error
}

func (f *fakeAdmissionExportSystem) Publish(ctx context.Context, _, _ string, message interface{}) error {
	if f.publish != nil {
		return f.publish(ctx, message)
	}
	if f.published != nil {
		f.published <- message
	}
	return f.publishErr
}

func (f *fakeAdmissionExportSystem) UpsertConnection(context.Context, interface{}, string, string) error {
	return nil
}

func (f *fakeAdmissionExportSystem) CloseConnection(string) error {
	return nil
}

type fakeAdmissionExportMetrics struct {
	mu            sync.Mutex
	queued        int
	queueFull     int
	published     int
	publishErrors int
	dropped       map[string]int
	queueDepth    int64
	queueBytes    int64
}

func (f *fakeAdmissionExportMetrics) reportAdmissionExportQueued() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queued++
}

func (f *fakeAdmissionExportMetrics) reportAdmissionExportQueueFull() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queueFull++
}

func (f *fakeAdmissionExportMetrics) reportAdmissionExportPublished() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published++
}

func (f *fakeAdmissionExportMetrics) reportAdmissionExportPublishError() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.publishErrors++
}

func (f *fakeAdmissionExportMetrics) reportAdmissionExportDropped(reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dropped == nil {
		f.dropped = make(map[string]int)
	}
	f.dropped[reason]++
}

func (f *fakeAdmissionExportMetrics) setAdmissionExportQueue(depth, bytes int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queueDepth = depth
	f.queueBytes = bytes
}

type admissionExportStatusReport struct {
	connectionName string
	status         statusv1alpha1.ConnectionPublishStatus
}

type fakeAdmissionExportStatusReporter struct {
	reports chan admissionExportStatusReport
}

func (f *fakeAdmissionExportStatusReporter) Report(_ context.Context, connectionName string, status statusv1alpha1.ConnectionPublishStatus) error {
	f.reports <- admissionExportStatusReport{
		connectionName: connectionName,
		status:         status,
	}
	return nil
}

func TestQueuedAdmissionViolationExporterDropsWhenQueueIsFull(t *testing.T) {
	metrics := &fakeAdmissionExportMetrics{}
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{}, "connection", "channel", logr.Discard(), metrics, nil)
	exporter.queue = make(chan queuedAdmissionViolation, 1)

	exporter.Export(&exportutil.ExportMsg{Message: "first"})
	exporter.Export(&exportutil.ExportMsg{Message: "second"})

	require.Equal(t, 1, metrics.queued)
	require.Equal(t, 1, metrics.queueFull)
	require.Equal(t, 1, metrics.dropped[admissionExportDropReasonQueueFull])
	require.Len(t, exporter.queue, 1)
}

func TestQueuedAdmissionViolationExporterDropsOversizedMessage(t *testing.T) {
	metrics := &fakeAdmissionExportMetrics{}
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{}, "connection", "channel", logr.Discard(), metrics, nil)
	exporter.maxMessageBytes = 1

	exporter.Export(&exportutil.ExportMsg{Message: "too large"})

	require.Zero(t, metrics.queued)
	require.Equal(t, 1, metrics.dropped[admissionExportDropReasonMessageTooLarge])
	require.Empty(t, exporter.queue)
}

func TestQueuedAdmissionViolationExporterDropsUnencodableMessage(t *testing.T) {
	metrics := &fakeAdmissionExportMetrics{}
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{}, "connection", "channel", logr.Discard(), metrics, nil)

	exporter.Export(&exportutil.ExportMsg{Details: func() {}})

	require.Zero(t, metrics.queued)
	require.Equal(t, 1, metrics.dropped[admissionExportDropReasonMarshalError])
	require.Empty(t, exporter.queue)
}

func TestQueuedAdmissionViolationExporterDropsWhenByteLimitIsReached(t *testing.T) {
	metrics := &fakeAdmissionExportMetrics{}
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{}, "connection", "channel", logr.Discard(), metrics, nil)
	exporter.maxQueueBytes = 1

	exporter.Export(&exportutil.ExportMsg{Message: "too large for queue"})

	require.Zero(t, metrics.queued)
	require.Equal(t, 1, metrics.queueFull)
	require.Equal(t, 1, metrics.dropped[admissionExportDropReasonQueueBytesFull])
	require.Empty(t, exporter.queue)
}

func TestQueuedAdmissionViolationExporterReportsPublishFailure(t *testing.T) {
	publishErr := errors.New("backend unavailable")
	metrics := &fakeAdmissionExportMetrics{}
	statusReporter := &fakeAdmissionExportStatusReporter{reports: make(chan admissionExportStatusReport, 1)}
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{publishErr: publishErr}, "audit-connection", "admission-channel", logr.Discard(), metrics, statusReporter)
	exporter.statusInterval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- exporter.Start(ctx)
	}()
	exporter.Export(&exportutil.ExportMsg{Message: "violation"})

	select {
	case report := <-statusReporter.reports:
		require.Equal(t, "audit-connection", report.connectionName)
		require.Equal(t, statusv1alpha1.WebhookPublishSource, report.status.Source)
		require.False(t, report.status.Active)
		require.Equal(t, []*statusv1alpha1.ConnectionError{
			{Type: statusv1alpha1.PublishError, Message: "backend unavailable"},
		}, report.status.Errors)
		require.NotNil(t, report.status.LastAttemptTime)
		require.Nil(t, report.status.LastSuccessTime)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for status report")
	}
	require.Equal(t, 1, metrics.publishErrors)

	cancel()
	require.NoError(t, <-done)
}

func TestAdmissionExportPublishStateBoundsErrors(t *testing.T) {
	nextError := 0
	system := &fakeAdmissionExportSystem{
		publish: func(context.Context, interface{}) error {
			err := fmt.Errorf("class-%03d: backend unavailable", nextError)
			nextError++
			return err
		},
	}
	exporter := newQueuedAdmissionViolationExporter(system, "connection", "channel", logr.Discard(), &fakeAdmissionExportMetrics{}, nil)
	state := admissionExportPublishState{errors: make(map[string]error)}

	for i := 0; i < exportutil.MaxConnectionStatusErrors+100; i++ {
		exporter.publishQueued(context.Background(), queuedAdmissionViolation{}, &state)
	}

	require.Len(t, state.errors, exportutil.MaxConnectionStatusErrors)
	require.Contains(t, state.errors, exportutil.AdditionalPublishErrorsOmittedMessage)
}

func TestQueuedAdmissionViolationExporterReportsPublishSuccess(t *testing.T) {
	metrics := &fakeAdmissionExportMetrics{}
	statusReporter := &fakeAdmissionExportStatusReporter{reports: make(chan admissionExportStatusReport, 1)}
	system := &fakeAdmissionExportSystem{published: make(chan interface{}, 1)}
	exporter := newQueuedAdmissionViolationExporter(system, "audit-connection", "admission-channel", logr.Discard(), metrics, statusReporter)
	exporter.statusInterval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- exporter.Start(ctx)
	}()
	exporter.Export(&exportutil.ExportMsg{Message: "violation"})

	select {
	case report := <-statusReporter.reports:
		require.Equal(t, statusv1alpha1.WebhookPublishSource, report.status.Source)
		require.True(t, report.status.Active)
		require.Empty(t, report.status.Errors)
		require.NotNil(t, report.status.LastAttemptTime)
		require.NotNil(t, report.status.LastSuccessTime)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for status report")
	}
	require.Equal(t, 1, metrics.published)
	require.Zero(t, metrics.queueDepth)
	require.Zero(t, metrics.queueBytes)
	published, ok := (<-system.published).(json.RawMessage)
	require.True(t, ok)
	require.JSONEq(t, `{"message":"violation"}`, string(published))

	cancel()
	require.NoError(t, <-done)
}

func TestQueuedAdmissionViolationExporterDrainsQueueOnShutdown(t *testing.T) {
	metrics := &fakeAdmissionExportMetrics{}
	system := &fakeAdmissionExportSystem{published: make(chan interface{}, 2)}
	exporter := newQueuedAdmissionViolationExporter(system, "audit-connection", "admission-channel", logr.Discard(), metrics, nil)
	exporter.Export(&exportutil.ExportMsg{Message: "first"})
	exporter.Export(&exportutil.ExportMsg{Message: "second"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, exporter.Start(ctx))

	require.Equal(t, 2, metrics.published)
	require.Zero(t, metrics.dropped[admissionExportDropReasonShutdown])
	require.Zero(t, metrics.queueDepth)
	require.Zero(t, metrics.queueBytes)
	require.Len(t, system.published, 2)

	exporter.Export(&exportutil.ExportMsg{Message: "after shutdown"})
	require.Equal(t, 1, metrics.dropped[admissionExportDropReasonShutdown])
}

func TestQueuedAdmissionViolationExporterCountsShutdownDrainTimeout(t *testing.T) {
	metrics := &fakeAdmissionExportMetrics{}
	system := &fakeAdmissionExportSystem{
		publish: func(ctx context.Context, _ interface{}) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	exporter := newQueuedAdmissionViolationExporter(system, "audit-connection", "admission-channel", logr.Discard(), metrics, nil)
	exporter.shutdownTimeout = time.Millisecond
	exporter.Export(&exportutil.ExportMsg{Message: "first"})
	exporter.Export(&exportutil.ExportMsg{Message: "second"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, exporter.Start(ctx))

	require.Equal(t, 1, metrics.publishErrors)
	require.Equal(t, 1, metrics.dropped[admissionExportDropReasonShutdown])
	require.Zero(t, metrics.queueDepth)
	require.Zero(t, metrics.queueBytes)
}

func TestQueuedAdmissionViolationExporterBoundsShutdownWhenBackendIgnoresContext(t *testing.T) {
	metrics := &fakeAdmissionExportMetrics{}
	publishStarted := make(chan struct{})
	releasePublish := make(chan struct{})
	publishDone := make(chan struct{})
	system := &fakeAdmissionExportSystem{
		publish: func(context.Context, interface{}) error {
			close(publishStarted)
			defer close(publishDone)
			<-releasePublish
			return nil
		},
	}
	exporter := newQueuedAdmissionViolationExporter(system, "audit-connection", "admission-channel", logr.Discard(), metrics, nil)
	exporter.shutdownTimeout = 10 * time.Millisecond
	exporter.Export(&exportutil.ExportMsg{Message: "first"})
	exporter.Export(&exportutil.ExportMsg{Message: "second"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		done <- exporter.Start(ctx)
	}()

	select {
	case <-publishStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for publish to start")
	}
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		close(releasePublish)
		<-publishDone
		t.Fatal("shutdown exceeded its drain timeout")
	}

	close(releasePublish)
	<-publishDone
	require.Zero(t, metrics.published)
	require.Equal(t, 1, metrics.publishErrors)
	require.Equal(t, 1, metrics.dropped[admissionExportDropReasonShutdown])
	require.Zero(t, metrics.queueDepth)
	require.Zero(t, metrics.queueBytes)
}

func TestQueuedAdmissionViolationExporterRateLimitsLogs(t *testing.T) {
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{}, "connection", "channel", logr.Discard(), &fakeAdmissionExportMetrics{}, nil)

	require.True(t, exporter.shouldLog(&exporter.lastDropLog))
	require.False(t, exporter.shouldLog(&exporter.lastDropLog))
}
