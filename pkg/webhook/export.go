package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	statusv1alpha1 "github.com/open-policy-agent/gatekeeper/v3/apis/status/v1alpha1"
	exportcontroller "github.com/open-policy-agent/gatekeeper/v3/pkg/controller/export"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/export"
	exportutil "github.com/open-policy-agent/gatekeeper/v3/pkg/export/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultAdmissionExportQueueSize = 1024
	// Bound the complete encoded record, not only the violation message. 64 KiB
	// leaves room for normal policy and request metadata while preventing one
	// user-influenced result from consuming a disproportionate share of the queue.
	defaultAdmissionExportMaxMessageBytes = 64 * 1024
	defaultAdmissionExportMaxQueueBytes   = 16 * 1024 * 1024
	defaultAdmissionExportStatusInterval  = 10 * time.Second
	defaultAdmissionExportLogInterval     = time.Minute
	admissionExportStatusTimeout          = 2 * time.Second
	admissionExportShutdownTimeout        = 5 * time.Second

	admissionExportDropReasonMarshalError    = "marshal_error"
	admissionExportDropReasonMessageTooLarge = "message_too_large"
	admissionExportDropReasonQueueBytesFull  = "queue_bytes_full"
	admissionExportDropReasonQueueFull       = "queue_full"
	admissionExportDropReasonShutdown        = "shutdown"
)

var (
	errAdmissionExportQueueFull       = errors.New("admission violation export queue is full")
	errAdmissionExportQueueBytesFull  = errors.New("admission violation export queue byte limit is reached")
	errAdmissionExportMessageTooLarge = errors.New("admission violation export message is too large")
)

type admissionViolationExporter interface {
	Export(*exportutil.ExportMsg)
}

type admissionExportMetrics interface {
	reportAdmissionExportQueued()
	reportAdmissionExportQueueFull()
	reportAdmissionExportPublished()
	reportAdmissionExportPublishError()
	reportAdmissionExportDropped(reason string)
	setAdmissionExportQueue(depth, bytes int64)
}

type admissionExportStatusReporter interface {
	Report(ctx context.Context, connectionName string, status statusv1alpha1.ConnectionPublishStatus) error
}

type queuedAdmissionViolation struct {
	data json.RawMessage
	size int64
}

// queuedAdmissionViolationExporter decouples backend latency from admission.
// Count and byte bounds make overload explicit rather than allowing unbounded
// memory growth in the webhook process.
type queuedAdmissionViolationExporter struct {
	system         export.Exporter
	connectionName string
	channel        string
	log            logr.Logger
	metrics        admissionExportMetrics
	statusReporter admissionExportStatusReporter
	queue          chan queuedAdmissionViolation
	// queueMu keeps shutdown from setting stopped between byte reservation and
	// enqueue, which would otherwise leave an unaccounted message in the queue.
	queueMu sync.RWMutex
	stopped bool
	// queueBytes includes bytes reserved by concurrent exporters that have not
	// necessarily completed their channel send yet.
	queueBytes      atomic.Int64
	maxMessageBytes int64
	maxQueueBytes   int64
	statusInterval  time.Duration
	logInterval     time.Duration
	shutdownTimeout time.Duration
	now             func() time.Time
	lastDropLog     atomic.Int64
	lastPublishLog  atomic.Int64
}

func newQueuedAdmissionViolationExporter(system export.Exporter, connectionName, channel string, log logr.Logger, metrics admissionExportMetrics, statusReporter admissionExportStatusReporter) *queuedAdmissionViolationExporter {
	return &queuedAdmissionViolationExporter{
		system:          system,
		connectionName:  connectionName,
		channel:         channel,
		log:             log,
		metrics:         metrics,
		statusReporter:  statusReporter,
		queue:           make(chan queuedAdmissionViolation, defaultAdmissionExportQueueSize),
		maxMessageBytes: defaultAdmissionExportMaxMessageBytes,
		maxQueueBytes:   defaultAdmissionExportMaxQueueBytes,
		statusInterval:  defaultAdmissionExportStatusInterval,
		logInterval:     defaultAdmissionExportLogInterval,
		shutdownTimeout: admissionExportShutdownTimeout,
		now:             time.Now,
	}
}

// Export runs on the admission path. It encodes and attempts a non-blocking
// enqueue, but never waits for backend I/O or queue capacity.
func (exporter *queuedAdmissionViolationExporter) Export(message *exportutil.ExportMsg) {
	encoded, err := json.Marshal(message)
	if err != nil {
		exporter.metrics.reportAdmissionExportDropped(admissionExportDropReasonMarshalError)
		exporter.logDrop(err, "dropping admission violation export because the message cannot be encoded")
		return
	}

	size := int64(len(encoded))
	if size > exporter.maxMessageBytes {
		exporter.metrics.reportAdmissionExportDropped(admissionExportDropReasonMessageTooLarge)
		exporter.logDrop(errAdmissionExportMessageTooLarge, "dropping admission violation export", "size", size, "limit", exporter.maxMessageBytes)
		return
	}
	exporter.queueMu.RLock()
	defer exporter.queueMu.RUnlock()
	if exporter.stopped {
		exporter.metrics.reportAdmissionExportDropped(admissionExportDropReasonShutdown)
		exporter.logDrop(context.Canceled, "dropping admission violation export because the exporter is stopping")
		return
	}
	if !exporter.reserveQueueBytes(size) {
		exporter.metrics.reportAdmissionExportQueueFull()
		exporter.metrics.reportAdmissionExportDropped(admissionExportDropReasonQueueBytesFull)
		exporter.logDrop(errAdmissionExportQueueBytesFull, "dropping admission violation export", "size", size, "limit", exporter.maxQueueBytes)
		return
	}

	select {
	case exporter.queue <- queuedAdmissionViolation{data: json.RawMessage(encoded), size: size}:
		exporter.metrics.reportAdmissionExportQueued()
		exporter.reportQueueState()
	default:
		exporter.queueBytes.Add(-size)
		exporter.metrics.reportAdmissionExportQueueFull()
		exporter.metrics.reportAdmissionExportDropped(admissionExportDropReasonQueueFull)
		exporter.reportQueueState()
		exporter.logDrop(errAdmissionExportQueueFull, "dropping admission violation export")
	}
}

// reserveQueueBytes atomically enforces the byte limit across concurrent webhook
// requests before any of them enqueue their message.
func (exporter *queuedAdmissionViolationExporter) reserveQueueBytes(size int64) bool {
	for {
		current := exporter.queueBytes.Load()
		if current+size > exporter.maxQueueBytes {
			return false
		}
		if exporter.queueBytes.CompareAndSwap(current, current+size) {
			return true
		}
	}
}

// reportQueueState records approximate gauges; concurrent producers can change
// channel depth between the depth and byte snapshots.
func (exporter *queuedAdmissionViolationExporter) reportQueueState() {
	exporter.metrics.setAdmissionExportQueue(int64(len(exporter.queue)), exporter.queueBytes.Load())
}

func (exporter *queuedAdmissionViolationExporter) logDrop(err error, message string, keysAndValues ...interface{}) {
	if exporter.shouldLog(&exporter.lastDropLog) {
		exporter.log.Error(err, message, keysAndValues...)
	}
}

func (exporter *queuedAdmissionViolationExporter) logPublishError(err error) {
	if exporter.shouldLog(&exporter.lastPublishLog) {
		exporter.log.Error(err, "failed to export admission violation")
	}
}

func (exporter *queuedAdmissionViolationExporter) shouldLog(lastLog *atomic.Int64) bool {
	now := time.Now().UnixNano()
	last := lastLog.Load()
	if last != 0 && time.Duration(now-last) < exporter.logInterval {
		return false
	}
	return lastLog.CompareAndSwap(last, now)
}

// admissionExportPublishState accumulates health between status intervals;
// errors are coalesced by class to avoid one status entry per failed message.
type admissionExportPublishState struct {
	attempted       bool
	active          bool
	errors          map[string]error
	lastAttemptTime time.Time
	lastSuccessTime time.Time
}

// Start publishes queued records serially and batches Connection status updates.
// It implements manager.Runnable and drains for a bounded interval on shutdown.
// Backend and status errors are reported through status, logs, and metrics rather
// than terminating the manager, so Start returns nil after shutdown.
func (exporter *queuedAdmissionViolationExporter) Start(ctx context.Context) error {
	ticker := time.NewTicker(exporter.statusInterval)
	defer ticker.Stop()
	state := admissionExportPublishState{errors: make(map[string]error)}

	for {
		// Give cancellation priority over a continuously ready queue. A select
		// alone could keep choosing records while the cancellation case is ready.
		if ctx.Err() != nil {
			exporter.shutdown(&state, nil)
			return nil
		}
		select {
		case <-ctx.Done():
			exporter.shutdown(&state, nil)
			return nil
		case queued := <-exporter.queue:
			if ctx.Err() != nil {
				exporter.shutdown(&state, &queued)
				return nil
			}
			exporter.publishQueued(ctx, queued, &state)
		case <-ticker.C:
			statusCtx, cancel := context.WithTimeout(ctx, admissionExportStatusTimeout)
			exporter.reportPublishStatus(statusCtx, &state)
			cancel()
		}
	}
}

func (exporter *queuedAdmissionViolationExporter) publishQueued(ctx context.Context, queued queuedAdmissionViolation, state *admissionExportPublishState) {
	exporter.queueBytes.Add(-queued.size)
	exporter.reportQueueState()
	state.attempted = true
	state.lastAttemptTime = exporter.now().UTC()
	if err := exporter.publish(ctx, queued.data); err != nil {
		exporter.metrics.reportAdmissionExportPublishError()
		exporter.logPublishError(err)
		state.errors = exportutil.AddPublishError(state.errors, err)
		return
	}
	exporter.metrics.reportAdmissionExportPublished()
	state.active = true
	state.lastSuccessTime = exporter.now().UTC()
}

func (exporter *queuedAdmissionViolationExporter) publish(ctx context.Context, data json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() {
		// Keep late completion isolated from exporter state. The buffered channel
		// lets this goroutine exit if the caller has already stopped waiting.
		result <- exporter.system.Publish(ctx, exporter.connectionName, exporter.channel, data)
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// shutdown first prevents new enqueues, then drains with a fresh timeout because
// the manager context is already canceled. Any remainder is counted as dropped.
func (exporter *queuedAdmissionViolationExporter) shutdown(state *admissionExportPublishState, selected *queuedAdmissionViolation) {
	exporter.queueMu.Lock()
	exporter.stopped = true
	exporter.queueMu.Unlock()

	drainCtx, cancelDrain := context.WithTimeout(context.Background(), exporter.shutdownTimeout)
	if selected != nil {
		exporter.publishQueued(drainCtx, *selected, state)
	}
	for drainCtx.Err() == nil {
		select {
		case queued := <-exporter.queue:
			exporter.publishQueued(drainCtx, queued, state)
		default:
			cancelDrain()
			exporter.flushPublishStatus(state)
			return
		}
	}
	cancelDrain()

	dropped := 0
	for {
		select {
		case queued := <-exporter.queue:
			exporter.queueBytes.Add(-queued.size)
			exporter.metrics.reportAdmissionExportDropped(admissionExportDropReasonShutdown)
			dropped++
		default:
			exporter.reportQueueState()
			if dropped > 0 {
				exporter.log.Error(context.DeadlineExceeded, "dropping queued admission violation exports after shutdown drain timed out", "count", dropped)
			}
			exporter.flushPublishStatus(state)
			return
		}
	}
}

func (exporter *queuedAdmissionViolationExporter) flushPublishStatus(state *admissionExportPublishState) {
	flushCtx, cancel := context.WithTimeout(context.Background(), admissionExportStatusTimeout)
	defer cancel()
	exporter.reportPublishStatus(flushCtx, state)
}

// reportPublishStatus updates per-pod health only after a publish attempt. State
// is cleared only after the status write succeeds so transient conflicts retry.
func (exporter *queuedAdmissionViolationExporter) reportPublishStatus(ctx context.Context, state *admissionExportPublishState) {
	if exporter.statusReporter == nil || !state.attempted {
		return
	}

	keys := make([]string, 0, len(state.errors))
	for key := range state.errors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	exportErrors := make([]*statusv1alpha1.ConnectionError, 0, len(keys))
	for _, key := range keys {
		exportErrors = append(exportErrors, &statusv1alpha1.ConnectionError{
			Type:    statusv1alpha1.PublishError,
			Message: exportutil.PublishErrorKey(state.errors[key]),
		})
	}
	lastAttemptTime := metav1.NewTime(state.lastAttemptTime)
	publishStatus := statusv1alpha1.ConnectionPublishStatus{
		Source:          statusv1alpha1.WebhookPublishSource,
		Active:          state.active,
		LastAttemptTime: &lastAttemptTime,
		Errors:          exportErrors,
	}
	if !state.lastSuccessTime.IsZero() {
		lastSuccessTime := metav1.NewTime(state.lastSuccessTime)
		publishStatus.LastSuccessTime = &lastSuccessTime
	}
	if err := exporter.statusReporter.Report(ctx, exporter.connectionName, publishStatus); err != nil {
		exporter.logPublishError(fmt.Errorf("reporting admission export connection status: %w", err))
		return
	}
	state.attempted = false
	state.active = false
	clear(state.errors)
}

// connectionStatusReporter attributes publish health to the pod that owns this
// in-process exporter rather than to another audit or webhook replica.
type connectionStatusReporter struct {
	reader client.Reader
	writer client.Writer
	scheme *runtime.Scheme
	getPod func(context.Context) (*corev1.Pod, error)
}

func (reporter *connectionStatusReporter) Report(ctx context.Context, connectionName string, status statusv1alpha1.ConnectionPublishStatus) error {
	return exportcontroller.UpdateConnectionPodPublishStatus(ctx, reporter.reader, reporter.writer, reporter.scheme, connectionName, status, reporter.getPod)
}
