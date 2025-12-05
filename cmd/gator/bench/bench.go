package bench

import (
	"fmt"
	"os"
	"strings"
	"time"

	cmdutils "github.com/open-policy-agent/gatekeeper/v3/cmd/gator/util"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/gator/bench"
	"github.com/spf13/cobra"
)

const (
	examples = `# Benchmark policies with default settings (1000 iterations, rego engine)
gator bench --filename="policies/"

# Benchmark with both Rego and CEL engines
gator bench --filename="policies/" --engine=all

# Benchmark with custom iterations and warmup
gator bench --filename="policies/" --iterations=500 --warmup=50

# Benchmark with concurrent load (simulates real webhook traffic)
gator bench --filename="policies/" --concurrency=10

# Output results as JSON
gator bench --filename="policies/" --output=json

# Benchmark policies from multiple sources
gator bench --filename="templates/" --filename="constraints/" --filename="resources/"

# Benchmark from OCI image
gator bench --image="ghcr.io/example/policies:latest"

# Benchmark with memory profiling
gator bench --filename="policies/" --memory

# Save benchmark results as baseline
gator bench --filename="policies/" --save=baseline.json

# Compare against baseline (fail if >10% regression or >1ms absolute increase)
gator bench --filename="policies/" --compare=baseline.json --threshold=10 --min-threshold=1ms`
)

// Cmd is the cobra command for the bench subcommand.
var Cmd = &cobra.Command{
	Use:   "bench",
	Short: "Benchmark policy evaluation performance",
	Long: `Benchmark evaluates the performance of Gatekeeper policies by running
constraint evaluation against test resources and measuring latency metrics.

This command loads ConstraintTemplates, Constraints, and Kubernetes resources
from the specified files or directories, then repeatedly evaluates the resources
against the constraints to gather performance statistics.

Supports both Rego and CEL policy engines for comparison.`,
	Example: examples,
	Run:     run,
	Args:    cobra.NoArgs,
}

var (
	flagFilenames    []string
	flagImages       []string
	flagTempDir      string
	flagEngine       string
	flagIterations   int
	flagWarmup       int
	flagConcurrency  int
	flagOutput       string
	flagStats        bool
	flagMemory       bool
	flagSave         string
	flagCompare      string
	flagThreshold    float64
	flagMinThreshold time.Duration
)

const (
	flagNameFilename     = "filename"
	flagNameImage        = "image"
	flagNameTempDir      = "tempdir"
	flagNameEngine       = "engine"
	flagNameIterations   = "iterations"
	flagNameWarmup       = "warmup"
	flagNameConcurrency  = "concurrency"
	flagNameOutput       = "output"
	flagNameStats        = "stats"
	flagNameMemory       = "memory"
	flagNameSave         = "save"
	flagNameCompare      = "compare"
	flagNameThreshold    = "threshold"
	flagNameMinThreshold = "min-threshold"
)

func init() {
	Cmd.Flags().StringArrayVarP(&flagFilenames, flagNameFilename, "f", []string{},
		"a file or directory containing ConstraintTemplates, Constraints, and resources to benchmark. Can be specified multiple times.")
	Cmd.Flags().StringArrayVarP(&flagImages, flagNameImage, "i", []string{},
		"a URL to an OCI image containing policies. Can be specified multiple times.")
	Cmd.Flags().StringVarP(&flagTempDir, flagNameTempDir, "d", "",
		"temporary directory to download and unpack images to.")
	Cmd.Flags().StringVarP(&flagEngine, flagNameEngine, "e", "rego",
		fmt.Sprintf("policy engine to benchmark. One of: %s|%s|%s", bench.EngineRego, bench.EngineCEL, bench.EngineAll))
	Cmd.Flags().IntVarP(&flagIterations, flagNameIterations, "n", 1000,
		"number of benchmark iterations to run. Use at least 1000 for meaningful P99 metrics.")
	Cmd.Flags().IntVar(&flagWarmup, flagNameWarmup, 10,
		"number of warmup iterations before measurement.")
	Cmd.Flags().IntVarP(&flagConcurrency, flagNameConcurrency, "c", 1,
		"number of concurrent goroutines for reviews. Higher values simulate realistic webhook load.")
	Cmd.Flags().StringVarP(&flagOutput, flagNameOutput, "o", "table",
		"output format. One of: table|json|yaml")
	Cmd.Flags().BoolVar(&flagStats, flagNameStats, false,
		"gather detailed statistics from the constraint framework.")
	Cmd.Flags().BoolVar(&flagMemory, flagNameMemory, false,
		"enable memory profiling to track allocations per review.")
	Cmd.Flags().StringVar(&flagSave, flagNameSave, "",
		"save benchmark results to this file for future comparison (supports .json and .yaml).")
	Cmd.Flags().StringVar(&flagCompare, flagNameCompare, "",
		"compare results against a baseline file (supports .json and .yaml).")
	Cmd.Flags().Float64Var(&flagThreshold, flagNameThreshold, 10.0,
		"regression threshold percentage for comparison. Exit code 1 if exceeded.")
	Cmd.Flags().DurationVar(&flagMinThreshold, flagNameMinThreshold, 0,
		"minimum absolute latency difference to consider a regression (e.g., 1ms). Prevents false positives on fast policies.")
}

func run(_ *cobra.Command, _ []string) {
	// Validate engine flag
	engine, err := parseEngine(flagEngine)
	if err != nil {
		cmdutils.ErrFatalf("invalid engine: %v", err)
	}

	// Validate output format
	outputFormat, err := bench.ParseOutputFormat(flagOutput)
	if err != nil {
		cmdutils.ErrFatalf("invalid output format: %v", err)
	}

	// Validate inputs
	if len(flagFilenames) == 0 && len(flagImages) == 0 {
		cmdutils.ErrFatalf("at least one --filename or --image must be specified")
	}

	if flagIterations <= 0 {
		cmdutils.ErrFatalf("iterations must be positive")
	}

	if flagWarmup < 0 {
		cmdutils.ErrFatalf("warmup must be non-negative")
	}

	if flagThreshold < 0 {
		cmdutils.ErrFatalf("threshold must be non-negative")
	}

	if flagConcurrency < 1 {
		cmdutils.ErrFatalf("concurrency must be at least 1")
	}

	// Run benchmark
	opts := &bench.Opts{
		Filenames:    flagFilenames,
		Images:       flagImages,
		TempDir:      flagTempDir,
		Engine:       engine,
		Iterations:   flagIterations,
		Warmup:       flagWarmup,
		Concurrency:  flagConcurrency,
		GatherStats:  flagStats,
		Memory:       flagMemory,
		Save:         flagSave,
		Baseline:     flagCompare,
		Threshold:    flagThreshold,
		MinThreshold: flagMinThreshold,
		Writer:       os.Stderr,
	}

	results, err := bench.Run(opts)
	if err != nil {
		cmdutils.ErrFatalf("benchmark failed: %v", err)
	}

	// Format and print results
	output, err := bench.FormatResults(results, outputFormat)
	if err != nil {
		cmdutils.ErrFatalf("formatting results: %v", err)
	}

	fmt.Print(output)

	// Save results if requested
	if flagSave != "" {
		if err := bench.SaveResults(results, flagSave); err != nil {
			cmdutils.ErrFatalf("saving results: %v", err)
		}
		fmt.Fprintf(os.Stderr, "\nResults saved to: %s\n", flagSave)
	}

	// Compare against baseline if requested
	exitCode := 0
	if flagCompare != "" {
		baseline, err := bench.LoadBaseline(flagCompare)
		if err != nil {
			cmdutils.ErrFatalf("loading baseline: %v", err)
		}

		comparisons := bench.Compare(baseline, results, flagThreshold, flagMinThreshold)
		if len(comparisons) == 0 {
			fmt.Fprintf(os.Stderr, "\nWarning: No matching engines found for comparison\n")
		} else {
			fmt.Println()
			fmt.Print(bench.FormatComparison(comparisons, flagThreshold))

			// Check if any comparison failed
			for _, comp := range comparisons {
				if !comp.Passed {
					exitCode = 1
					break
				}
			}
		}
	}

	os.Exit(exitCode)
}

func parseEngine(s string) (bench.Engine, error) {
	switch strings.ToLower(s) {
	case "rego":
		return bench.EngineRego, nil
	case "cel":
		return bench.EngineCEL, nil
	case "all":
		return bench.EngineAll, nil
	default:
		return "", fmt.Errorf("invalid engine %q (valid: rego, cel, all)", s)
	}
}
