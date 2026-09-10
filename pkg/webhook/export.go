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
	"unicode/utf8"

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
	defaultAdmissionExportBatchSize = 64
	// defaultAdmissionExportMaxMessageBytes bounds the complete encoded record,
	// not only the violation message. The limit is measured against the full
	// JSON-marshaled ExportMsg: the violation message and constraint annotations,
	// but also details emitted by the policy, every label on the reviewed
	// resource, and the request identity (user, groups, UID).
	//
	// 64 KiB is a good limit because it is generous for realistic policies while
	// still capping the fields an author or requester can inflate without bound.
	// A typical violation from community policy libraries encodes to only a few
	// kilobytes (short messages, empty or tiny details, a handful of
	// annotations), so 64 KiB leaves roughly an order of magnitude of headroom
	// for normal policy and request metadata. At the same time the two fields
	// that are not bounded by any policy catalog -- the policy-provided details
	// and the reviewed resource's labels -- cannot grow the record without limit:
	// a single hostile or verbose result is prevented from consuming a
	// disproportionate share of the in-memory queue and the on-disk spool, which
	// are themselves bounded (see defaultAdmissionExportMaxQueueBytes and the
	// disk spool limits). Records above the limit are dropped before enqueue and
	// reported with drop reason message_too_large rather than silently truncated,
	// so a torn partial record is never exported.
	defaultAdmissionExportMaxMessageBytes = 64 * 1024
	defaultAdmissionExportMaxQueueBytes   = 16 * 1024 * 1024
	defaultAdmissionExportStatusInterval  = 10 * time.Second
	defaultAdmissionExportHealthyInterval = time.Minute
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
	TryExport(func() *exportutil.ExportMsg)
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
	stateMu sync.Mutex
	state   admissionExportPublishState
	// queueBytes includes bytes reserved by concurrent exporters that have not
	// necessarily completed their channel send yet.
	queueMessages    atomic.Int64
	queueBytes       atomic.Int64
	maxMessageBytes  int64
	maxQueueBytes    int64
	maxBatchSize     int
	statusInterval   time.Duration
	healthyInterval  time.Duration
	logInterval      time.Duration
	shutdownTimeout  time.Duration
	now              func() time.Time
	lastDropLog      atomic.Int64
	lastPublishLog   atomic.Int64
	statusReported   bool
	lastStatusTime   time.Time
	lastStatusActive bool
	lastStatusErrors bool
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
		maxBatchSize:    defaultAdmissionExportBatchSize,
		statusInterval:  defaultAdmissionExportStatusInterval,
		healthyInterval: defaultAdmissionExportHealthyInterval,
		logInterval:     defaultAdmissionExportLogInterval,
		shutdownTimeout: admissionExportShutdownTimeout,
		now:             time.Now,
		state:           newAdmissionExportPublishState(),
	}
}

// Export is a convenience wrapper for callers that already own a message.
func (exporter *queuedAdmissionViolationExporter) Export(message *exportutil.ExportMsg) {
	exporter.TryExport(func() *exportutil.ExportMsg { return message })
}

// TryExport runs on the admission path. It reserves count capacity before
// building the message, then encodes and attempts a non-blocking enqueue.
func (exporter *queuedAdmissionViolationExporter) TryExport(build func() *exportutil.ExportMsg) {
	exporter.queueMu.RLock()
	defer exporter.queueMu.RUnlock()
	if exporter.stopped {
		exporter.metrics.reportAdmissionExportDropped(admissionExportDropReasonShutdown)
		exporter.logDrop(context.Canceled, "dropping admission violation export because the exporter is stopping")
		return
	}
	if !exporter.reserveQueueMessage() {
		exporter.metrics.reportAdmissionExportQueueFull()
		exporter.metrics.reportAdmissionExportDropped(admissionExportDropReasonQueueFull)
		exporter.logDrop(errAdmissionExportQueueFull, "dropping admission violation export")
		return
	}
	reservedMessage := true
	defer func() {
		if reservedMessage {
			exporter.queueMessages.Add(-1)
		}
	}()

	message := build()
	if !admissionExportMessageFitsBudget(message, exporter.maxMessageBytes) {
		exporter.metrics.reportAdmissionExportDropped(admissionExportDropReasonMessageTooLarge)
		exporter.logDrop(errAdmissionExportMessageTooLarge, "dropping admission violation export", "limit", exporter.maxMessageBytes)
		return
	}
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
	if !exporter.reserveQueueBytes(size) {
		exporter.metrics.reportAdmissionExportQueueFull()
		exporter.metrics.reportAdmissionExportDropped(admissionExportDropReasonQueueBytesFull)
		exporter.logDrop(errAdmissionExportQueueBytesFull, "dropping admission violation export", "size", size, "limit", exporter.maxQueueBytes)
		return
	}

	select {
	case exporter.queue <- queuedAdmissionViolation{data: json.RawMessage(encoded), size: size}:
		reservedMessage = false
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

// admissionExportMessageFitsBudget computes a lower bound for JSON-compatible
// values produced by policy evaluation. It only rejects messages that cannot
// fit; unknown or borderline values fall through to json.Marshal.
func admissionExportMessageFitsBudget(message *exportutil.ExportMsg, limit int64) bool {
	if message == nil {
		return true
	}
	budget := limit
	stringsToEncode := []string{
		message.ID,
		message.EventType,
		message.Group,
		message.Version,
		message.Kind,
		message.Name,
		message.Namespace,
		message.Message,
		message.EnforcementAction,
		message.ResourceGroup,
		message.ResourceAPIVersion,
		message.ResourceKind,
		message.ResourceNamespace,
		message.ResourceName,
		message.Timestamp,
		message.Operation,
		message.RequestResource,
		message.RequestSubresource,
		message.RequestUsername,
		message.RequestUserUID,
	}
	for _, value := range stringsToEncode {
		if value != "" && !consumeAdmissionExportJSONString(&budget, value) {
			return false
		}
	}
	for _, values := range [][]string{message.EnforcementActions, message.RequestUserGroups} {
		if len(values) == 0 {
			continue
		}
		if !consumeAdmissionExportBudget(&budget, int64(len(values))+1) {
			return false
		}
		for _, value := range values {
			if !consumeAdmissionExportJSONString(&budget, value) {
				return false
			}
		}
	}
	for _, values := range []map[string]string{message.ConstraintAnnotations, message.ResourceLabels} {
		if len(values) == 0 {
			continue
		}
		if !consumeAdmissionExportBudget(&budget, int64(len(values))*2+1) {
			return false
		}
		for key, value := range values {
			if !consumeAdmissionExportJSONString(&budget, key) || !consumeAdmissionExportJSONString(&budget, value) {
				return false
			}
		}
	}
	if message.Details == nil {
		return true
	}

	nodes := 4096
	withinBudget, known := consumeAdmissionExportJSONValue(&budget, message.Details, 0, &nodes)
	return !known || withinBudget
}

func consumeAdmissionExportJSONValue(budget *int64, value interface{}, depth int, nodes *int) (bool, bool) {
	if depth > 64 || *nodes == 0 {
		return false, false
	}
	*nodes--
	switch typed := value.(type) {
	case nil:
		return consumeAdmissionExportBudget(budget, 4), true
	case bool:
		return consumeAdmissionExportBudget(budget, 4), true
	case string:
		return consumeAdmissionExportJSONString(budget, typed), true
	case json.Number:
		return false, false
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return consumeAdmissionExportBudget(budget, 1), true
	case json.RawMessage:
		return false, false
	case []byte:
		encoded := int64(2 + 4*((len(typed)+2)/3))
		return consumeAdmissionExportBudget(budget, encoded), true
	case []string:
		if len(typed) > 0 && !consumeAdmissionExportBudget(budget, int64(len(typed))+1) {
			return false, true
		}
		for _, item := range typed {
			if !consumeAdmissionExportJSONString(budget, item) {
				return false, true
			}
		}
		return true, true
	case []interface{}:
		if len(typed) > 0 && !consumeAdmissionExportBudget(budget, int64(len(typed))+1) {
			return false, true
		}
		for _, item := range typed {
			withinBudget, known := consumeAdmissionExportJSONValue(budget, item, depth+1, nodes)
			if !known || !withinBudget {
				return withinBudget, known
			}
		}
		return true, true
	case map[string]string:
		if len(typed) > 0 && !consumeAdmissionExportBudget(budget, int64(len(typed))*2+1) {
			return false, true
		}
		for key, item := range typed {
			if !consumeAdmissionExportJSONString(budget, key) || !consumeAdmissionExportJSONString(budget, item) {
				return false, true
			}
		}
		return true, true
	case map[string]interface{}:
		if len(typed) > 0 && !consumeAdmissionExportBudget(budget, int64(len(typed))*2+1) {
			return false, true
		}
		for key, item := range typed {
			if !consumeAdmissionExportJSONString(budget, key) {
				return false, true
			}
			withinBudget, known := consumeAdmissionExportJSONValue(budget, item, depth+1, nodes)
			if !known || !withinBudget {
				return withinBudget, known
			}
		}
		return true, true
	default:
		return false, false
	}
}

func consumeAdmissionExportBudget(budget *int64, size int64) bool {
	if size < 0 || size > *budget {
		return false
	}
	*budget -= size
	return true
}

func consumeAdmissionExportJSONString(budget *int64, value string) bool {
	if !consumeAdmissionExportBudget(budget, 2) {
		return false
	}
	for index := 0; index < len(value); {
		char := value[index]
		if char < utf8.RuneSelf {
			size := int64(1)
			switch char {
			case '\\', '"', '\n', '\r', '\t', '\b', '\f':
				size = 2
			case '<', '>', '&':
				size = 6
			default:
				if char < 0x20 {
					size = 6
				}
			}
			if !consumeAdmissionExportBudget(budget, size) {
				return false
			}
			index++
			continue
		}
		runeValue, size := utf8.DecodeRuneInString(value[index:])
		encodedSize := int64(size)
		if runeValue == utf8.RuneError && size == 1 || runeValue == '\u2028' || runeValue == '\u2029' {
			encodedSize = 6
		}
		if !consumeAdmissionExportBudget(budget, encodedSize) {
			return false
		}
		index += size
	}
	return true
}

func (exporter *queuedAdmissionViolationExporter) reserveQueueMessage() bool {
	limit := int64(cap(exporter.queue))
	for {
		current := exporter.queueMessages.Load()
		if current >= limit {
			return false
		}
		if exporter.queueMessages.CompareAndSwap(current, current+1) {
			return true
		}
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
	statusDone := make(chan struct{})
	if exporter.statusReporter == nil {
		close(statusDone)
	} else {
		go func() {
			defer close(statusDone)
			exporter.runStatusReporter(ctx)
		}()
	}

	for {
		// Give cancellation priority over a continuously ready queue. A select
		// alone could keep choosing records while the cancellation case is ready.
		if ctx.Err() != nil {
			<-statusDone
			exporter.shutdown(nil)
			return nil
		}
		select {
		case <-ctx.Done():
			<-statusDone
			exporter.shutdown(nil)
			return nil
		case queued := <-exporter.queue:
			if ctx.Err() != nil {
				<-statusDone
				exporter.shutdown(&queued)
				return nil
			}
			exporter.publishQueuedBatch(ctx, exporter.collectQueuedBatch(queued))
		}
	}
}

func (exporter *queuedAdmissionViolationExporter) runStatusReporter(ctx context.Context) {
	ticker := time.NewTicker(exporter.statusInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			statusCtx, cancel := context.WithTimeout(ctx, admissionExportStatusTimeout)
			exporter.reportPendingPublishStatus(statusCtx, false)
			cancel()
		}
	}
}

func (exporter *queuedAdmissionViolationExporter) collectQueuedBatch(first queuedAdmissionViolation) []queuedAdmissionViolation {
	batch := make([]queuedAdmissionViolation, 1, exporter.maxBatchSize)
	batch[0] = first
	for len(batch) < exporter.maxBatchSize {
		select {
		case queued := <-exporter.queue:
			batch = append(batch, queued)
		default:
			return batch
		}
	}
	return batch
}

func (exporter *queuedAdmissionViolationExporter) publishQueued(ctx context.Context, queued queuedAdmissionViolation) {
	exporter.publishQueuedBatch(ctx, []queuedAdmissionViolation{queued})
}

func (exporter *queuedAdmissionViolationExporter) publishQueuedBatch(ctx context.Context, queued []queuedAdmissionViolation) {
	messages := make([]any, len(queued))
	for i := range queued {
		exporter.queueMessages.Add(-1)
		exporter.queueBytes.Add(-queued[i].size)
		messages[i] = queued[i].data
	}
	exporter.reportQueueState()

	errorsByMessage := exporter.publishBatch(ctx, messages)
	for _, err := range errorsByMessage {
		exporter.recordPublishResult(err)
	}
}

func (exporter *queuedAdmissionViolationExporter) publishBatch(ctx context.Context, messages []any) []error {
	if err := ctx.Err(); err != nil {
		return admissionExportPublishErrors(len(messages), err)
	}
	result := make(chan []error, 1)
	go func() {
		// Keep late completion isolated from exporter state. The buffered channel
		// lets this goroutine exit if the caller has already stopped waiting.
		result <- exporter.publishBatchUnbounded(ctx, messages)
	}()
	select {
	case errorsByMessage := <-result:
		return errorsByMessage
	case <-ctx.Done():
		return admissionExportPublishErrors(len(messages), ctx.Err())
	}
}

func (exporter *queuedAdmissionViolationExporter) publishBatchUnbounded(ctx context.Context, messages []any) []error {
	errorsByMessage := make([]error, len(messages))
	if batchExporter, ok := exporter.system.(export.BatchExporter); ok && len(messages) > 1 {
		errorsByMessage = batchExporter.PublishBatch(ctx, exporter.connectionName, exporter.channel, messages)
		if len(errorsByMessage) != len(messages) {
			resultCount := len(errorsByMessage)
			errorsByMessage = make([]error, len(messages))
			for i := range errorsByMessage {
				errorsByMessage[i] = fmt.Errorf("batch exporter returned %d results for %d messages", resultCount, len(messages))
			}
		}
	} else {
		for i := range messages {
			errorsByMessage[i] = exporter.system.Publish(ctx, exporter.connectionName, exporter.channel, messages[i])
		}
	}
	return errorsByMessage
}

func admissionExportPublishErrors(count int, err error) []error {
	errorsByMessage := make([]error, count)
	for i := range errorsByMessage {
		errorsByMessage[i] = err
	}
	return errorsByMessage
}

func (exporter *queuedAdmissionViolationExporter) recordPublishResult(err error) {
	now := exporter.now().UTC()
	if err != nil {
		exporter.metrics.reportAdmissionExportPublishError()
		exporter.logPublishError(err)
	} else {
		exporter.metrics.reportAdmissionExportPublished()
	}

	exporter.stateMu.Lock()
	defer exporter.stateMu.Unlock()
	exporter.state.attempted = true
	exporter.state.lastAttemptTime = now
	if err != nil {
		exporter.state.errors = exportutil.AddPublishError(exporter.state.errors, err)
		return
	}
	exporter.state.active = true
	exporter.state.lastSuccessTime = now
}

// shutdown first prevents new enqueues, then drains with a fresh timeout because
// the manager context is already canceled. Any remainder is counted as dropped.
func (exporter *queuedAdmissionViolationExporter) shutdown(selected *queuedAdmissionViolation) {
	exporter.queueMu.Lock()
	exporter.stopped = true
	exporter.queueMu.Unlock()

	drainCtx, cancelDrain := context.WithTimeout(context.Background(), exporter.shutdownTimeout)
	if selected != nil {
		exporter.publishQueued(drainCtx, *selected)
	}
	for drainCtx.Err() == nil {
		select {
		case queued := <-exporter.queue:
			exporter.publishQueued(drainCtx, queued)
		default:
			cancelDrain()
			exporter.flushPublishStatus()
			return
		}
	}
	cancelDrain()

	dropped := 0
	for {
		select {
		case queued := <-exporter.queue:
			exporter.queueMessages.Add(-1)
			exporter.queueBytes.Add(-queued.size)
			exporter.metrics.reportAdmissionExportDropped(admissionExportDropReasonShutdown)
			dropped++
		default:
			exporter.reportQueueState()
			if dropped > 0 {
				exporter.log.Error(context.DeadlineExceeded, "dropping queued admission violation exports after shutdown drain timed out", "count", dropped)
			}
			exporter.flushPublishStatus()
			return
		}
	}
}

func (exporter *queuedAdmissionViolationExporter) flushPublishStatus() {
	flushCtx, cancel := context.WithTimeout(context.Background(), admissionExportStatusTimeout)
	defer cancel()
	exporter.reportPendingPublishStatus(flushCtx, true)
}

func newAdmissionExportPublishState() admissionExportPublishState {
	return admissionExportPublishState{errors: make(map[string]error)}
}

// reportPendingPublishStatus swaps the accumulated state before API I/O so the
// publisher never waits for status. Failed snapshots merge back with newer work.
func (exporter *queuedAdmissionViolationExporter) reportPendingPublishStatus(ctx context.Context, force bool) {
	if exporter.statusReporter == nil {
		return
	}
	exporter.stateMu.Lock()
	if !exporter.state.attempted {
		exporter.stateMu.Unlock()
		return
	}
	if !force && exporter.statusReported && !exporter.lastStatusErrors && len(exporter.state.errors) == 0 && exporter.state.active == exporter.lastStatusActive && exporter.now().Sub(exporter.lastStatusTime) < exporter.healthyInterval {
		exporter.stateMu.Unlock()
		return
	}
	state := exporter.state
	exporter.state = newAdmissionExportPublishState()
	exporter.stateMu.Unlock()

	if err := exporter.reportPublishStatus(ctx, &state); err != nil {
		exporter.logPublishError(fmt.Errorf("reporting admission export connection status: %w", err))
		exporter.stateMu.Lock()
		exporter.mergePublishStateLocked(state)
		exporter.stateMu.Unlock()
		return
	}
	exporter.statusReported = true
	exporter.lastStatusTime = exporter.now()
	exporter.lastStatusActive = state.active
	exporter.lastStatusErrors = len(state.errors) > 0
}

func (exporter *queuedAdmissionViolationExporter) mergePublishStateLocked(previous admissionExportPublishState) {
	exporter.state.attempted = exporter.state.attempted || previous.attempted
	exporter.state.active = exporter.state.active || previous.active
	if previous.lastAttemptTime.After(exporter.state.lastAttemptTime) {
		exporter.state.lastAttemptTime = previous.lastAttemptTime
	}
	if previous.lastSuccessTime.After(exporter.state.lastSuccessTime) {
		exporter.state.lastSuccessTime = previous.lastSuccessTime
	}
	for _, err := range previous.errors {
		exporter.state.errors = exportutil.AddPublishError(exporter.state.errors, err)
	}
}

func (exporter *queuedAdmissionViolationExporter) reportPublishStatus(ctx context.Context, state *admissionExportPublishState) error {
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
	return exporter.statusReporter.Report(ctx, exporter.connectionName, publishStatus)
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
