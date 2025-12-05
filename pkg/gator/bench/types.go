package bench

import (
	"io"
	"time"
)

// Engine represents the policy evaluation engine to benchmark.
type Engine string

const (
	// EngineRego benchmarks the Rego/OPA policy engine.
	EngineRego Engine = "rego"
	// EngineCEL benchmarks the Kubernetes CEL policy engine.
	EngineCEL Engine = "cel"
	// EngineAll benchmarks both Rego and CEL engines.
	EngineAll Engine = "all"
)

// Opts configures the benchmark run.
type Opts struct {
	// Filenames are the paths to files or directories containing
	// ConstraintTemplates, Constraints, and objects to review.
	Filenames []string

	// Images are OCI image URLs containing policies.
	Images []string

	// TempDir is the directory for unpacking OCI images.
	TempDir string

	// Engine specifies which policy engine(s) to benchmark.
	Engine Engine

	// Iterations is the number of review cycles to run.
	Iterations int

	// Warmup is the number of warmup iterations before measurement.
	Warmup int

	// GatherStats enables collection of per-constraint statistics
	// from the constraint framework.
	GatherStats bool

	// Memory enables memory profiling during benchmark.
	Memory bool

	// Baseline is the path to a baseline results file for comparison.
	Baseline string

	// Save is the path to save benchmark results for future comparison.
	Save string

	// Threshold is the regression threshold percentage for comparison.
	// If a metric regresses more than this percentage, the benchmark fails.
	Threshold float64

	// Writer is where warnings and informational messages are written.
	// If nil, warnings are not printed.
	Writer io.Writer
}

// Results contains benchmark metrics for a single engine.
type Results struct {
	// Engine is the policy engine that was benchmarked.
	Engine Engine `json:"engine" yaml:"engine"`

	// TemplateCount is the number of ConstraintTemplates loaded.
	TemplateCount int `json:"templateCount" yaml:"templateCount"`

	// SkippedTemplates contains names of templates skipped due to engine incompatibility.
	SkippedTemplates []string `json:"skippedTemplates,omitempty" yaml:"skippedTemplates,omitempty"`

	// ConstraintCount is the number of Constraints loaded.
	ConstraintCount int `json:"constraintCount" yaml:"constraintCount"`

	// SkippedConstraints contains names of constraints skipped due to missing templates.
	SkippedConstraints []string `json:"skippedConstraints,omitempty" yaml:"skippedConstraints,omitempty"`

	// ObjectCount is the number of objects reviewed.
	ObjectCount int `json:"objectCount" yaml:"objectCount"`

	// Iterations is the number of review cycles run.
	Iterations int `json:"iterations" yaml:"iterations"`

	// SetupDuration is the total time taken to load templates, constraints, and data.
	SetupDuration time.Duration `json:"setupDuration" yaml:"setupDuration"`

	// SetupBreakdown contains detailed timing for each setup phase.
	SetupBreakdown SetupBreakdown `json:"setupBreakdown" yaml:"setupBreakdown"`

	// TotalDuration is the total time for all review iterations.
	TotalDuration time.Duration `json:"totalDuration" yaml:"totalDuration"`

	// Latencies contains timing for each review operation.
	Latencies Latencies `json:"latencies" yaml:"latencies"`

	// ViolationCount is the total number of violations found.
	ViolationCount int `json:"violationCount" yaml:"violationCount"`

	// ReviewsPerSecond is the throughput metric (reviews/second).
	ReviewsPerSecond float64 `json:"reviewsPerSecond" yaml:"reviewsPerSecond"`

	// MemoryStats contains memory allocation statistics (only populated with --memory).
	MemoryStats *MemoryStats `json:"memoryStats,omitempty" yaml:"memoryStats,omitempty"`
}

// SetupBreakdown contains detailed timing for setup phases.
type SetupBreakdown struct {
	// ClientCreation is the time to create the constraint client.
	ClientCreation time.Duration `json:"clientCreation" yaml:"clientCreation"`

	// TemplateCompilation is the time to compile all templates.
	TemplateCompilation time.Duration `json:"templateCompilation" yaml:"templateCompilation"`

	// ConstraintLoading is the time to load all constraints.
	ConstraintLoading time.Duration `json:"constraintLoading" yaml:"constraintLoading"`

	// DataLoading is the time to load reference data.
	DataLoading time.Duration `json:"dataLoading" yaml:"dataLoading"`
}

// Latencies contains latency statistics.
type Latencies struct {
	// Min is the minimum latency observed.
	Min time.Duration `json:"min" yaml:"min"`

	// Max is the maximum latency observed.
	Max time.Duration `json:"max" yaml:"max"`

	// Mean is the average latency.
	Mean time.Duration `json:"mean" yaml:"mean"`

	// P50 is the 50th percentile (median) latency.
	P50 time.Duration `json:"p50" yaml:"p50"`

	// P95 is the 95th percentile latency.
	P95 time.Duration `json:"p95" yaml:"p95"`

	// P99 is the 99th percentile latency.
	P99 time.Duration `json:"p99" yaml:"p99"`
}

// MemoryStats contains memory allocation statistics from benchmark runs.
type MemoryStats struct {
	// AllocsPerReview is the average number of allocations per review.
	AllocsPerReview uint64 `json:"allocsPerReview" yaml:"allocsPerReview"`

	// BytesPerReview is the average bytes allocated per review.
	BytesPerReview uint64 `json:"bytesPerReview" yaml:"bytesPerReview"`

	// TotalAllocs is the total number of allocations during measurement.
	TotalAllocs uint64 `json:"totalAllocs" yaml:"totalAllocs"`

	// TotalBytes is the total bytes allocated during measurement.
	TotalBytes uint64 `json:"totalBytes" yaml:"totalBytes"`
}

// ComparisonResult contains the result of comparing current results against a baseline.
type ComparisonResult struct {
	// BaselineEngine is the engine from the baseline.
	BaselineEngine Engine `json:"baselineEngine" yaml:"baselineEngine"`

	// CurrentEngine is the engine from the current run.
	CurrentEngine Engine `json:"currentEngine" yaml:"currentEngine"`

	// Metrics contains the comparison for each metric.
	Metrics []MetricComparison `json:"metrics" yaml:"metrics"`

	// Passed indicates whether all metrics are within threshold.
	Passed bool `json:"passed" yaml:"passed"`

	// FailedMetrics contains names of metrics that exceeded threshold.
	FailedMetrics []string `json:"failedMetrics,omitempty" yaml:"failedMetrics,omitempty"`
}

// MetricComparison contains comparison data for a single metric.
type MetricComparison struct {
	// Name is the metric name.
	Name string `json:"name" yaml:"name"`

	// Baseline is the baseline value.
	Baseline float64 `json:"baseline" yaml:"baseline"`

	// Current is the current value.
	Current float64 `json:"current" yaml:"current"`

	// Delta is the percentage change (positive = regression for latency, negative = improvement).
	Delta float64 `json:"delta" yaml:"delta"`

	// Passed indicates whether this metric is within threshold.
	Passed bool `json:"passed" yaml:"passed"`
}
