package bench

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-policy-agent/frameworks/constraint/pkg/apis"
	constraintclient "github.com/open-policy-agent/frameworks/constraint/pkg/client"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/drivers/rego"
	clienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/frameworks/constraint/pkg/client/reviews"
	"github.com/open-policy-agent/frameworks/constraint/pkg/instrumentation"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/drivers/k8scel"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/reader"
	mutationtypes "github.com/open-policy-agent/gatekeeper/v3/pkg/mutation/types"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/target"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
)

const (
	// MinIterationsForP99 is the minimum number of iterations recommended for
	// statistically meaningful P99 metrics.
	MinIterationsForP99 = 1000
)

var scheme *k8sruntime.Scheme

func init() {
	scheme = k8sruntime.NewScheme()
	if err := apis.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

// Run executes the benchmark with the given options and returns results
// for each engine tested.
func Run(opts *Opts) ([]Results, error) {
	// Warn if iterations are too low for meaningful P99 statistics
	if opts.Iterations < MinIterationsForP99 && opts.Writer != nil {
		fmt.Fprintf(opts.Writer, "Warning: %d iterations may not provide statistically meaningful P99 metrics. Consider using at least %d iterations.\n\n",
			opts.Iterations, MinIterationsForP99)
	}

	// Default concurrency to 1 (sequential)
	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}

	// Read all resources from files/images
	objs, err := reader.ReadSources(opts.Filenames, opts.Images, opts.TempDir)
	if err != nil {
		return nil, fmt.Errorf("reading sources: %w", err)
	}
	if len(objs) == 0 {
		return nil, fmt.Errorf("no input data identified")
	}

	// Categorize objects
	var templates []*unstructured.Unstructured
	var constraints []*unstructured.Unstructured
	var reviewObjs []*unstructured.Unstructured

	for _, obj := range objs {
		switch {
		case reader.IsTemplate(obj):
			templates = append(templates, obj)
		case reader.IsConstraint(obj):
			constraints = append(constraints, obj)
		default:
			// Everything else is a potential review object
			reviewObjs = append(reviewObjs, obj)
		}
	}

	if len(templates) == 0 {
		return nil, fmt.Errorf("no ConstraintTemplates found in input")
	}
	if len(constraints) == 0 {
		return nil, fmt.Errorf("no Constraints found in input")
	}
	if len(reviewObjs) == 0 {
		return nil, fmt.Errorf("no objects to review found in input")
	}

	var results []Results
	var warnings []string

	// Determine which engines to benchmark
	engines := []Engine{opts.Engine}
	if opts.Engine == EngineAll {
		engines = []Engine{EngineRego, EngineCEL}
	}

	for _, engine := range engines {
		result, err := runBenchmark(engine, templates, constraints, reviewObjs, opts)
		if err != nil {
			// For "all" engine mode, record warning and continue with other engines
			if opts.Engine == EngineAll {
				warnings = append(warnings, fmt.Sprintf("%s: %s", engine, err.Error()))
				continue
			}
			return nil, fmt.Errorf("benchmarking %s: %w", engine, err)
		}
		results = append(results, *result)
	}

	// Check if we have any results
	if len(results) == 0 {
		return nil, fmt.Errorf("no engines could process the templates: %v", warnings)
	}

	// Add warnings about skipped engines to the first result for visibility
	if len(warnings) > 0 && len(results) > 0 && opts.Writer != nil {
		for _, w := range warnings {
			fmt.Fprintf(opts.Writer, "Warning: Engine skipped - %s\n", w)
		}
		fmt.Fprintln(opts.Writer)
	}

	return results, nil
}

// runBenchmark runs the benchmark for a single engine.
func runBenchmark(
	engine Engine,
	templates []*unstructured.Unstructured,
	constraints []*unstructured.Unstructured,
	reviewObjs []*unstructured.Unstructured,
	opts *Opts,
) (*Results, error) {
	ctx := context.Background()
	var setupBreakdown SetupBreakdown
	var skippedTemplates []string
	var skippedConstraints []string
	loadedTemplateKinds := make(map[string]bool)

	// Create the client for this engine
	setupStart := time.Now()
	clientStart := time.Now()
	client, err := makeClient(engine, opts.GatherStats)
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}
	setupBreakdown.ClientCreation = time.Since(clientStart)

	// Add templates (with skip support for incompatible templates)
	templateStart := time.Now()
	for _, obj := range templates {
		templ, err := reader.ToTemplate(scheme, obj)
		if err != nil {
			return nil, fmt.Errorf("converting template %q: %w", obj.GetName(), err)
		}
		_, err = client.AddTemplate(ctx, templ)
		if err != nil {
			// Check if this is an engine compatibility issue
			if errors.Is(err, clienterrors.ErrNoDriver) {
				skippedTemplates = append(skippedTemplates, obj.GetName())
				continue
			}
			return nil, fmt.Errorf("adding template %q: %w", templ.GetName(), err)
		}
		// Track the constraint kind this template creates
		loadedTemplateKinds[templ.Spec.CRD.Spec.Names.Kind] = true
	}
	setupBreakdown.TemplateCompilation = time.Since(templateStart)

	// Check if all templates were skipped
	loadedTemplateCount := len(templates) - len(skippedTemplates)
	if loadedTemplateCount == 0 {
		return nil, fmt.Errorf("no templates compatible with %s engine (all %d templates skipped)", engine, len(templates))
	}

	// Add constraints (skip those whose template was skipped)
	constraintStart := time.Now()
	for _, obj := range constraints {
		kind := obj.GetKind()
		if !loadedTemplateKinds[kind] {
			skippedConstraints = append(skippedConstraints, obj.GetName())
			continue
		}
		if _, err := client.AddConstraint(ctx, obj); err != nil {
			return nil, fmt.Errorf("adding constraint %q: %w", obj.GetName(), err)
		}
	}
	setupBreakdown.ConstraintLoading = time.Since(constraintStart)

	// Check if all constraints were skipped
	loadedConstraintCount := len(constraints) - len(skippedConstraints)
	if loadedConstraintCount == 0 {
		return nil, fmt.Errorf("no constraints loaded (all %d constraints skipped due to missing templates)", len(constraints))
	}

	// Add all objects as data (for referential constraints)
	// Note: CEL driver doesn't support referential constraints, so skip data loading for CEL
	dataStart := time.Now()
	var skippedDataObjects []string
	referentialDataSupported := engine != EngineCEL
	if referentialDataSupported {
		for _, obj := range reviewObjs {
			_, err := client.AddData(ctx, obj)
			if err != nil {
				return nil, fmt.Errorf("adding data %q: %w", obj.GetName(), err)
			}
		}
	}
	// Note: We don't populate skippedDataObjects for CEL engine because it's expected
	// behavior (CEL doesn't support referential data), not an error. The
	// ReferentialDataSupported field indicates this engine limitation.
	setupBreakdown.DataLoading = time.Since(dataStart)

	setupDuration := time.Since(setupStart)

	// Warmup phase
	for i := 0; i < opts.Warmup; i++ {
		for _, obj := range reviewObjs {
			au := target.AugmentedUnstructured{
				Object: *obj,
				Source: mutationtypes.SourceTypeOriginal,
			}
			if _, err := client.Review(ctx, au, reviews.EnforcementPoint(util.GatorEnforcementPoint)); err != nil {
				return nil, fmt.Errorf("warmup review failed: %w", err)
			}
		}
	}

	// Measurement phase
	var durations []time.Duration
	var totalViolations int64

	// Memory profiling: capture memory stats before and after
	var memStatsBefore, memStatsAfter runtime.MemStats
	if opts.Memory {
		runtime.GC() // Run GC to get clean baseline
		runtime.ReadMemStats(&memStatsBefore)
	}

	benchStart := time.Now()

	// Concurrent or sequential execution based on concurrency setting
	var statsEntries []*instrumentation.StatsEntry
	if opts.Concurrency > 1 {
		durations, totalViolations, statsEntries, err = runConcurrentBenchmark(ctx, client, reviewObjs, opts)
		if err != nil {
			return nil, err
		}
	} else {
		durations, totalViolations, statsEntries, err = runSequentialBenchmark(ctx, client, reviewObjs, opts)
		if err != nil {
			return nil, err
		}
	}

	totalDuration := time.Since(benchStart)

	// Capture memory stats after measurement
	var memStats *MemoryStats
	if opts.Memory {
		runtime.ReadMemStats(&memStatsAfter)
		totalReviewsForMem := uint64(opts.Iterations) * uint64(len(reviewObjs)) //nolint:gosec // overflow is acceptable for benchmark counts
		if totalReviewsForMem > 0 {
			totalAllocs := memStatsAfter.Mallocs - memStatsBefore.Mallocs
			totalBytes := memStatsAfter.TotalAlloc - memStatsBefore.TotalAlloc
			memStats = &MemoryStats{
				TotalAllocs:     totalAllocs,
				TotalBytes:      totalBytes,
				AllocsPerReview: totalAllocs / totalReviewsForMem,
				BytesPerReview:  totalBytes / totalReviewsForMem,
			}
		}
	}

	// Calculate metrics
	latencies := calculateLatencies(durations)
	totalReviews := opts.Iterations * len(reviewObjs)
	throughput := calculateThroughput(totalReviews, totalDuration)

	return &Results{
		Engine:                   engine,
		TemplateCount:            loadedTemplateCount,
		ConstraintCount:          loadedConstraintCount,
		ObjectCount:              len(reviewObjs),
		Iterations:               opts.Iterations,
		Concurrency:              opts.Concurrency,
		SetupDuration:            setupDuration,
		SetupBreakdown:           setupBreakdown,
		TotalDuration:            totalDuration,
		Latencies:                latencies,
		ViolationCount:           int(totalViolations),
		ReviewsPerSecond:         throughput,
		MemoryStats:              memStats,
		StatsEntries:             statsEntries,
		SkippedTemplates:         skippedTemplates,
		SkippedConstraints:       skippedConstraints,
		SkippedDataObjects:       skippedDataObjects,
		ReferentialDataSupported: referentialDataSupported,
	}, nil
}

// makeClient creates a constraint client configured for the specified engine.
func makeClient(engine Engine, gatherStats bool) (*constraintclient.Client, error) {
	args := []constraintclient.Opt{
		constraintclient.Targets(&target.K8sValidationTarget{}),
		constraintclient.EnforcementPoints(util.GatorEnforcementPoint),
	}

	switch engine {
	case EngineRego:
		driver, err := makeRegoDriver(gatherStats)
		if err != nil {
			return nil, err
		}
		args = append(args, constraintclient.Driver(driver))

	case EngineCEL:
		driver, err := makeCELDriver(gatherStats)
		if err != nil {
			return nil, err
		}
		args = append(args, constraintclient.Driver(driver))

	default:
		return nil, fmt.Errorf("unsupported engine: %s", engine)
	}

	return constraintclient.NewClient(args...)
}

func makeRegoDriver(gatherStats bool) (*rego.Driver, error) {
	var args []rego.Arg
	if gatherStats {
		args = append(args, rego.GatherStats())
	}
	return rego.New(args...)
}

func makeCELDriver(gatherStats bool) (*k8scel.Driver, error) {
	var args []k8scel.Arg
	if gatherStats {
		args = append(args, k8scel.GatherStats())
	}
	return k8scel.New(args...)
}

// runSequentialBenchmark runs the benchmark sequentially (single-threaded).
func runSequentialBenchmark(
	ctx context.Context,
	client *constraintclient.Client,
	reviewObjs []*unstructured.Unstructured,
	opts *Opts,
) ([]time.Duration, int64, []*instrumentation.StatsEntry, error) {
	var durations []time.Duration
	var totalViolations int64
	var statsEntries []*instrumentation.StatsEntry

	for i := 0; i < opts.Iterations; i++ {
		for _, obj := range reviewObjs {
			au := target.AugmentedUnstructured{
				Object: *obj,
				Source: mutationtypes.SourceTypeOriginal,
			}

			reviewStart := time.Now()
			resp, err := client.Review(ctx, au, reviews.EnforcementPoint(util.GatorEnforcementPoint))
			reviewDuration := time.Since(reviewStart)

			if err != nil {
				return nil, 0, nil, fmt.Errorf("review failed for %s/%s: %w",
					obj.GetNamespace(), obj.GetName(), err)
			}

			durations = append(durations, reviewDuration)

			// Count violations
			for _, r := range resp.ByTarget {
				totalViolations += int64(len(r.Results))
			}

			// Collect stats only from first iteration to avoid excessive data
			if opts.GatherStats && i == 0 {
				statsEntries = append(statsEntries, resp.StatsEntries...)
			}
		}
	}

	return durations, totalViolations, statsEntries, nil
}

// reviewResult holds the result of a single review for concurrent execution.
type reviewResult struct {
	duration     time.Duration
	violations   int
	statsEntries []*instrumentation.StatsEntry
	err          error
}

// runConcurrentBenchmark runs the benchmark with multiple goroutines.
func runConcurrentBenchmark(
	ctx context.Context,
	client *constraintclient.Client,
	reviewObjs []*unstructured.Unstructured,
	opts *Opts,
) ([]time.Duration, int64, []*instrumentation.StatsEntry, error) {
	totalReviews := opts.Iterations * len(reviewObjs)

	// Create work items
	type workItem struct {
		iteration int
		objIndex  int
	}
	workChan := make(chan workItem, totalReviews)
	for i := 0; i < opts.Iterations; i++ {
		for j := range reviewObjs {
			workChan <- workItem{iteration: i, objIndex: j}
		}
	}
	close(workChan)

	// Result collection
	resultsChan := make(chan reviewResult, totalReviews)
	var wg sync.WaitGroup
	var firstErr atomic.Value

	// Launch worker goroutines
	for w := 0; w < opts.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for work := range workChan {
				// Check if we should stop due to an error
				if firstErr.Load() != nil {
					return
				}

				obj := reviewObjs[work.objIndex]
				au := target.AugmentedUnstructured{
					Object: *obj,
					Source: mutationtypes.SourceTypeOriginal,
				}

				reviewStart := time.Now()
				resp, err := client.Review(ctx, au, reviews.EnforcementPoint(util.GatorEnforcementPoint))
				reviewDuration := time.Since(reviewStart)

				if err != nil {
					firstErr.CompareAndSwap(nil, fmt.Errorf("review failed for %s/%s: %w",
						obj.GetNamespace(), obj.GetName(), err))
					resultsChan <- reviewResult{err: err}
					return
				}

				violations := 0
				for _, r := range resp.ByTarget {
					violations += len(r.Results)
				}

				// Collect stats only from first iteration to avoid excessive data
				var stats []*instrumentation.StatsEntry
				if opts.GatherStats && work.iteration == 0 {
					stats = resp.StatsEntries
				}

				resultsChan <- reviewResult{
					duration:     reviewDuration,
					violations:   violations,
					statsEntries: stats,
				}
			}
		}()
	}

	// Wait for all workers to complete and close results channel
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	var durations []time.Duration
	var totalViolations int64
	var statsEntries []*instrumentation.StatsEntry

	for result := range resultsChan {
		if result.err != nil {
			continue
		}
		durations = append(durations, result.duration)
		totalViolations += int64(result.violations)
		if len(result.statsEntries) > 0 {
			statsEntries = append(statsEntries, result.statsEntries...)
		}
	}

	// Check for errors
	if errVal := firstErr.Load(); errVal != nil {
		if err, ok := errVal.(error); ok {
			return nil, 0, nil, err
		}
	}

	return durations, totalViolations, statsEntries, nil
}
