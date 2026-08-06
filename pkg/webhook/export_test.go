package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	report  func(context.Context, string, statusv1alpha1.ConnectionPublishStatus) error
}

func (f *fakeAdmissionExportStatusReporter) Report(ctx context.Context, connectionName string, status statusv1alpha1.ConnectionPublishStatus) error {
	if f.report != nil {
		return f.report(ctx, connectionName, status)
	}
	f.reports <- admissionExportStatusReport{
		connectionName: connectionName,
		status:         status,
	}
	return nil
}

func TestQueuedAdmissionViolationExporterStatusDoesNotBlockPublishing(t *testing.T) {
	statusStarted := make(chan struct{})
	releaseStatus := make(chan struct{})
	var started sync.Once
	statusReporter := &fakeAdmissionExportStatusReporter{
		report: func(context.Context, string, statusv1alpha1.ConnectionPublishStatus) error {
			started.Do(func() { close(statusStarted) })
			<-releaseStatus
			return nil
		},
	}
	system := &fakeAdmissionExportSystem{published: make(chan interface{}, 2)}
	exporter := newQueuedAdmissionViolationExporter(system, "connection", "channel", logr.Discard(), &fakeAdmissionExportMetrics{}, statusReporter)
	exporter.statusInterval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- exporter.Start(ctx) }()
	exporter.Export(&exportutil.ExportMsg{Message: "first"})
	select {
	case <-system.published:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first publish")
	}
	select {
	case <-statusStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for status report")
	}

	exporter.Export(&exportutil.ExportMsg{Message: "second"})
	select {
	case <-system.published:
	case <-time.After(time.Second):
		t.Fatal("status report blocked the publisher")
	}

	close(releaseStatus)
	cancel()
	require.NoError(t, <-done)
}

func TestQueuedAdmissionViolationExporterRetainsFailedStatusSnapshot(t *testing.T) {
	reportErr := errors.New("status unavailable")
	reports := 0
	statusReporter := &fakeAdmissionExportStatusReporter{
		report: func(context.Context, string, statusv1alpha1.ConnectionPublishStatus) error {
			reports++
			if reports == 1 {
				return reportErr
			}
			return nil
		},
	}
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{}, "connection", "channel", logr.Discard(), &fakeAdmissionExportMetrics{}, statusReporter)
	exporter.recordPublishResult(nil)

	exporter.reportPendingPublishStatus(context.Background(), true)
	exporter.stateMu.Lock()
	require.True(t, exporter.state.attempted)
	exporter.stateMu.Unlock()

	exporter.reportPendingPublishStatus(context.Background(), true)
	exporter.stateMu.Lock()
	require.False(t, exporter.state.attempted)
	exporter.stateMu.Unlock()
	require.Equal(t, 2, reports)
}

func TestQueuedAdmissionViolationExporterCoalescesHealthyStatus(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	reports := 0
	statusReporter := &fakeAdmissionExportStatusReporter{
		report: func(context.Context, string, statusv1alpha1.ConnectionPublishStatus) error {
			reports++
			return nil
		},
	}
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{}, "connection", "channel", logr.Discard(), &fakeAdmissionExportMetrics{}, statusReporter)
	exporter.now = func() time.Time { return now }

	exporter.recordPublishResult(nil)
	exporter.reportPendingPublishStatus(context.Background(), false)
	require.Equal(t, 1, reports)

	now = now.Add(10 * time.Second)
	exporter.recordPublishResult(nil)
	exporter.reportPendingPublishStatus(context.Background(), false)
	require.Equal(t, 1, reports)

	now = now.Add(exporter.healthyInterval)
	exporter.reportPendingPublishStatus(context.Background(), false)
	require.Equal(t, 2, reports)
}

func TestQueuedAdmissionViolationExporterReportsRecoveryWithoutWaitingForHeartbeat(t *testing.T) {
	reports := 0
	statusReporter := &fakeAdmissionExportStatusReporter{
		report: func(context.Context, string, statusv1alpha1.ConnectionPublishStatus) error {
			reports++
			return nil
		},
	}
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{}, "connection", "channel", logr.Discard(), &fakeAdmissionExportMetrics{}, statusReporter)

	exporter.recordPublishResult(errors.New("backend unavailable"))
	exporter.reportPendingPublishStatus(context.Background(), false)
	exporter.recordPublishResult(nil)
	exporter.reportPendingPublishStatus(context.Background(), false)

	require.Equal(t, 2, reports)
}

func TestQueuedAdmissionViolationExporterDropsWhenQueueIsFull(t *testing.T) {
	metrics := &fakeAdmissionExportMetrics{}
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{}, "connection", "channel", logr.Discard(), metrics, nil)
	exporter.queue = make(chan queuedAdmissionViolation, 1)

	exporter.Export(&exportutil.ExportMsg{Message: "first"})
	built := false
	exporter.TryExport(func() *exportutil.ExportMsg {
		built = true
		return &exportutil.ExportMsg{Message: "second"}
	})

	require.False(t, built)
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

func TestQueuedAdmissionViolationExporterReleasesMessageReservationAfterOversizedDrop(t *testing.T) {
	metrics := &fakeAdmissionExportMetrics{}
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{}, "connection", "channel", logr.Discard(), metrics, nil)
	exporter.queue = make(chan queuedAdmissionViolation, 1)
	exporter.maxMessageBytes = 2048

	exporter.Export(&exportutil.ExportMsg{Details: map[string]interface{}{"value": strings.Repeat("x", 4096)}})
	exporter.Export(&exportutil.ExportMsg{Message: "fits"})

	require.Equal(t, 1, metrics.dropped[admissionExportDropReasonMessageTooLarge])
	require.Equal(t, 1, metrics.queued)
	require.Len(t, exporter.queue, 1)
}

func TestAdmissionExportMessagePreflightAllowsExactEncodedLimit(t *testing.T) {
	message := &exportutil.ExportMsg{
		Message: strings.Repeat("<&", 64),
		Details: map[string]interface{}{"missing": []interface{}{"team", true, float64(1)}},
	}
	encoded, err := json.Marshal(message)
	require.NoError(t, err)
	require.True(t, admissionExportMessageFitsBudget(message, int64(len(encoded))))
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
	exporter := newQueuedAdmissionViolationExporter(&fakeAdmissionExportSystem{}, "connection", "channel", logr.Discard(), &fakeAdmissionExportMetrics{}, nil)
	for i := 0; i < exportutil.MaxConnectionStatusErrors+100; i++ {
		exporter.recordPublishResult(fmt.Errorf("class-%03d: backend unavailable", i))
	}

	exporter.stateMu.Lock()
	defer exporter.stateMu.Unlock()
	require.Len(t, exporter.state.errors, exportutil.MaxConnectionStatusErrors)
	require.Contains(t, exporter.state.errors, exportutil.AdditionalPublishErrorsOmittedMessage)
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
