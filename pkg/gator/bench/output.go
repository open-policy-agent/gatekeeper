package bench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"
)

// OutputFormat represents the output format for benchmark results.
type OutputFormat string

const (
	// OutputFormatTable outputs results as a human-readable table.
	OutputFormatTable OutputFormat = "table"
	// OutputFormatJSON outputs results as JSON.
	OutputFormatJSON OutputFormat = "json"
	// OutputFormatYAML outputs results as YAML.
	OutputFormatYAML OutputFormat = "yaml"
)

// ParseOutputFormat parses a string into an OutputFormat.
func ParseOutputFormat(s string) (OutputFormat, error) {
	switch strings.ToLower(s) {
	case "", "table":
		return OutputFormatTable, nil
	case "json":
		return OutputFormatJSON, nil
	case "yaml":
		return OutputFormatYAML, nil
	default:
		return "", fmt.Errorf("invalid output format: %q (valid: table, json, yaml)", s)
	}
}

// FormatResults formats benchmark results according to the specified format.
func FormatResults(results []Results, format OutputFormat) (string, error) {
	switch format {
	case OutputFormatJSON:
		return formatJSON(results)
	case OutputFormatYAML:
		return formatYAML(results)
	case OutputFormatTable:
		fallthrough
	default:
		return formatTable(results), nil
	}
}

// FormatComparison formats comparison results for display.
func FormatComparison(comparisons []ComparisonResult, threshold float64) string {
	var buf bytes.Buffer

	for i, comp := range comparisons {
		if i > 0 {
			buf.WriteString("\n")
		}
		writeComparisonResult(&buf, &comp, threshold)
	}

	return buf.String()
}

func writeComparisonResult(w io.Writer, comp *ComparisonResult, threshold float64) {
	fmt.Fprintf(w, "=== Baseline Comparison: %s Engine ===\n\n",
		strings.ToUpper(string(comp.CurrentEngine)))

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	// Header
	fmt.Fprintln(tw, "Metric\tBaseline\tCurrent\tDelta\tStatus")
	fmt.Fprintln(tw, "------\t--------\t-------\t-----\t------")

	for _, m := range comp.Metrics {
		status := "✓"
		if !m.Passed {
			status = "✗ REGRESSION"
		}

		// Format values based on metric type
		var baselineStr, currentStr string
		switch {
		case strings.Contains(m.Name, "Latency"):
			baselineStr = formatDuration(time.Duration(m.Baseline))
			currentStr = formatDuration(time.Duration(m.Current))
		case strings.Contains(m.Name, "Bytes"):
			baselineStr = formatBytes(uint64(m.Baseline))
			currentStr = formatBytes(uint64(m.Current))
		case strings.Contains(m.Name, "Throughput"):
			baselineStr = fmt.Sprintf("%.2f/sec", m.Baseline)
			currentStr = fmt.Sprintf("%.2f/sec", m.Current)
		default:
			baselineStr = fmt.Sprintf("%.0f", m.Baseline)
			currentStr = fmt.Sprintf("%.0f", m.Current)
		}

		deltaStr := fmt.Sprintf("%+.1f%%", m.Delta)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			m.Name, baselineStr, currentStr, deltaStr, status)
	}
	tw.Flush()

	fmt.Fprintln(w)
	if comp.Passed {
		fmt.Fprintf(w, "✓ No significant regressions (threshold: %.1f%%)\n", threshold)
	} else {
		fmt.Fprintf(w, "✗ Regressions detected in: %s (threshold: %.1f%%)\n",
			strings.Join(comp.FailedMetrics, ", "), threshold)
	}
}

func formatJSON(results []Results) (string, error) {
	// Convert to JSON-friendly format with string durations
	jsonResults := toJSONResults(results)
	b, err := json.MarshalIndent(jsonResults, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling JSON: %w", err)
	}
	return string(b), nil
}

func formatYAML(results []Results) (string, error) {
	// Convert to YAML-friendly format with string durations
	yamlResults := toJSONResults(results)
	b, err := yaml.Marshal(yamlResults)
	if err != nil {
		return "", fmt.Errorf("marshaling YAML: %w", err)
	}
	return string(b), nil
}

func formatTable(results []Results) string {
	var buf bytes.Buffer

	// Write individual result tables
	for i := range results {
		if i > 0 {
			buf.WriteString("\n")
		}
		writeResultTable(&buf, &results[i])
	}

	// Write comparison table if multiple engines
	if len(results) > 1 {
		buf.WriteString("\n")
		writeComparisonTable(&buf, results)
	}

	return buf.String()
}

func writeResultTable(w io.Writer, r *Results) {
	fmt.Fprintf(w, "=== Benchmark Results: %s Engine ===\n\n", strings.ToUpper(string(r.Engine)))

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	// Configuration section
	fmt.Fprintln(tw, "Configuration:")
	fmt.Fprintf(tw, "  Templates:\t%d\n", r.TemplateCount)
	fmt.Fprintf(tw, "  Constraints:\t%d\n", r.ConstraintCount)
	fmt.Fprintf(tw, "  Objects:\t%d\n", r.ObjectCount)
	fmt.Fprintf(tw, "  Iterations:\t%d\n", r.Iterations)
	if r.Concurrency > 1 {
		fmt.Fprintf(tw, "  Concurrency:\t%d\n", r.Concurrency)
	}
	fmt.Fprintf(tw, "  Total Reviews:\t%d\n", r.Iterations*r.ObjectCount)
	fmt.Fprintln(tw)

	// Skipped templates/constraints warning
	if len(r.SkippedTemplates) > 0 || len(r.SkippedConstraints) > 0 {
		fmt.Fprintln(tw, "Warnings:")
		if len(r.SkippedTemplates) > 0 {
			fmt.Fprintf(tw, "  Skipped Templates:\t%d (%s)\n",
				len(r.SkippedTemplates), strings.Join(r.SkippedTemplates, ", "))
		}
		if len(r.SkippedConstraints) > 0 {
			fmt.Fprintf(tw, "  Skipped Constraints:\t%d (%s)\n",
				len(r.SkippedConstraints), strings.Join(r.SkippedConstraints, ", "))
		}
		fmt.Fprintln(tw)
	}

	// Timing section with breakdown
	fmt.Fprintln(tw, "Timing:")
	fmt.Fprintf(tw, "  Setup Duration:\t%s\n", formatDuration(r.SetupDuration))
	if r.SetupBreakdown.ClientCreation > 0 {
		fmt.Fprintf(tw, "    └─ Client Creation:\t%s\n", formatDuration(r.SetupBreakdown.ClientCreation))
		fmt.Fprintf(tw, "    └─ Template Compilation:\t%s\n", formatDuration(r.SetupBreakdown.TemplateCompilation))
		fmt.Fprintf(tw, "    └─ Constraint Loading:\t%s\n", formatDuration(r.SetupBreakdown.ConstraintLoading))
		fmt.Fprintf(tw, "    └─ Data Loading:\t%s\n", formatDuration(r.SetupBreakdown.DataLoading))
	}
	fmt.Fprintf(tw, "  Total Duration:\t%s\n", formatDuration(r.TotalDuration))
	fmt.Fprintf(tw, "  Throughput:\t%.2f reviews/sec\n", r.ReviewsPerSecond)
	fmt.Fprintln(tw)

	// Latency section
	fmt.Fprintln(tw, "Latency (per review):")
	fmt.Fprintf(tw, "  Min:\t%s\n", formatDuration(r.Latencies.Min))
	fmt.Fprintf(tw, "  Max:\t%s\n", formatDuration(r.Latencies.Max))
	fmt.Fprintf(tw, "  Mean:\t%s\n", formatDuration(r.Latencies.Mean))
	fmt.Fprintf(tw, "  P50:\t%s\n", formatDuration(r.Latencies.P50))
	fmt.Fprintf(tw, "  P95:\t%s\n", formatDuration(r.Latencies.P95))
	fmt.Fprintf(tw, "  P99:\t%s\n", formatDuration(r.Latencies.P99))
	fmt.Fprintln(tw)

	// Results section
	fmt.Fprintln(tw, "Results:")
	fmt.Fprintf(tw, "  Violations Found:\t%d\n", r.ViolationCount)

	// Memory section (if available)
	if r.MemoryStats != nil {
		fmt.Fprintln(tw)
		fmt.Fprintln(tw, "Memory:")
		fmt.Fprintf(tw, "  Allocs/Review:\t%d\n", r.MemoryStats.AllocsPerReview)
		fmt.Fprintf(tw, "  Bytes/Review:\t%s\n", formatBytes(r.MemoryStats.BytesPerReview))
		fmt.Fprintf(tw, "  Total Allocs:\t%d\n", r.MemoryStats.TotalAllocs)
		fmt.Fprintf(tw, "  Total Bytes:\t%s\n", formatBytes(r.MemoryStats.TotalBytes))
	}

	tw.Flush()
}

// writeComparisonTable writes a side-by-side comparison of engine results.
func writeComparisonTable(w io.Writer, results []Results) {
	fmt.Fprintln(w, "=== Engine Comparison ===")
	fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	// Header row
	fmt.Fprint(tw, "Metric")
	for i := range results {
		fmt.Fprintf(tw, "\t%s", strings.ToUpper(string(results[i].Engine)))
	}
	fmt.Fprintln(tw)

	// Separator
	fmt.Fprint(tw, "------")
	for range results {
		fmt.Fprint(tw, "\t------")
	}
	fmt.Fprintln(tw)

	// Templates
	fmt.Fprint(tw, "Templates")
	for i := range results {
		fmt.Fprintf(tw, "\t%d", results[i].TemplateCount)
	}
	fmt.Fprintln(tw)

	// Constraints
	fmt.Fprint(tw, "Constraints")
	for i := range results {
		fmt.Fprintf(tw, "\t%d", results[i].ConstraintCount)
	}
	fmt.Fprintln(tw)

	// Setup Duration
	fmt.Fprint(tw, "Setup Time")
	for i := range results {
		fmt.Fprintf(tw, "\t%s", formatDuration(results[i].SetupDuration))
	}
	fmt.Fprintln(tw)

	// Throughput
	fmt.Fprint(tw, "Throughput")
	for i := range results {
		fmt.Fprintf(tw, "\t%.2f/sec", results[i].ReviewsPerSecond)
	}
	fmt.Fprintln(tw)

	// Mean Latency
	fmt.Fprint(tw, "Mean Latency")
	for i := range results {
		fmt.Fprintf(tw, "\t%s", formatDuration(results[i].Latencies.Mean))
	}
	fmt.Fprintln(tw)

	// P95 Latency
	fmt.Fprint(tw, "P95 Latency")
	for i := range results {
		fmt.Fprintf(tw, "\t%s", formatDuration(results[i].Latencies.P95))
	}
	fmt.Fprintln(tw)

	// P99 Latency
	fmt.Fprint(tw, "P99 Latency")
	for i := range results {
		fmt.Fprintf(tw, "\t%s", formatDuration(results[i].Latencies.P99))
	}
	fmt.Fprintln(tw)

	// Violations
	fmt.Fprint(tw, "Violations")
	for i := range results {
		fmt.Fprintf(tw, "\t%d", results[i].ViolationCount)
	}
	fmt.Fprintln(tw)

	// Memory stats (if available)
	hasMemory := false
	for i := range results {
		if results[i].MemoryStats != nil {
			hasMemory = true
			break
		}
	}
	if hasMemory {
		fmt.Fprint(tw, "Allocs/Review")
		for i := range results {
			if results[i].MemoryStats != nil {
				fmt.Fprintf(tw, "\t%d", results[i].MemoryStats.AllocsPerReview)
			} else {
				fmt.Fprint(tw, "\t-")
			}
		}
		fmt.Fprintln(tw)

		fmt.Fprint(tw, "Bytes/Review")
		for i := range results {
			if results[i].MemoryStats != nil {
				fmt.Fprintf(tw, "\t%s", formatBytes(results[i].MemoryStats.BytesPerReview))
			} else {
				fmt.Fprint(tw, "\t-")
			}
		}
		fmt.Fprintln(tw)
	}

	tw.Flush()

	// Show performance difference if exactly 2 engines
	if len(results) == 2 {
		fmt.Fprintln(w)
		writePerfDiff(w, &results[0], &results[1])
	}
}

// writePerfDiff writes a performance comparison between two engines.
func writePerfDiff(w io.Writer, r1, r2 *Results) {
	// Calculate throughput ratio
	if r1.ReviewsPerSecond <= 0 || r2.ReviewsPerSecond <= 0 {
		return
	}

	switch {
	case r1.ReviewsPerSecond > r2.ReviewsPerSecond:
		ratio := r1.ReviewsPerSecond / r2.ReviewsPerSecond
		fmt.Fprintf(w, "Performance: %s is %.2fx faster than %s\n",
			strings.ToUpper(string(r1.Engine)), ratio, strings.ToUpper(string(r2.Engine)))
	case r2.ReviewsPerSecond > r1.ReviewsPerSecond:
		ratio := r2.ReviewsPerSecond / r1.ReviewsPerSecond
		fmt.Fprintf(w, "Performance: %s is %.2fx faster than %s\n",
			strings.ToUpper(string(r2.Engine)), ratio, strings.ToUpper(string(r1.Engine)))
	default:
		fmt.Fprintln(w, "Performance: Both engines have similar throughput")
	}
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fµs", float64(d.Nanoseconds())/1000)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1000000)
	}
	return fmt.Sprintf("%.3fs", d.Seconds())
}

// formatBytes formats bytes in a human-readable way.
func formatBytes(b uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.2f KB", float64(b)/KB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// JSONResults is a JSON/YAML-friendly version of Results with string durations.
type JSONResults struct {
	Engine             string             `json:"engine" yaml:"engine"`
	TemplateCount      int                `json:"templateCount" yaml:"templateCount"`
	ConstraintCount    int                `json:"constraintCount" yaml:"constraintCount"`
	ObjectCount        int                `json:"objectCount" yaml:"objectCount"`
	Iterations         int                `json:"iterations" yaml:"iterations"`
	Concurrency        int                `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	TotalReviews       int                `json:"totalReviews" yaml:"totalReviews"`
	SetupDuration      string             `json:"setupDuration" yaml:"setupDuration"`
	SetupBreakdown     JSONSetupBreakdown `json:"setupBreakdown" yaml:"setupBreakdown"`
	TotalDuration      string             `json:"totalDuration" yaml:"totalDuration"`
	Latencies          JSONLatency        `json:"latencies" yaml:"latencies"`
	ViolationCount     int                `json:"violationCount" yaml:"violationCount"`
	ReviewsPerSecond   float64            `json:"reviewsPerSecond" yaml:"reviewsPerSecond"`
	MemoryStats        *JSONMemoryStats   `json:"memoryStats,omitempty" yaml:"memoryStats,omitempty"`
	SkippedTemplates   []string           `json:"skippedTemplates,omitempty" yaml:"skippedTemplates,omitempty"`
	SkippedConstraints []string           `json:"skippedConstraints,omitempty" yaml:"skippedConstraints,omitempty"`
}

// JSONSetupBreakdown is a JSON/YAML-friendly version of SetupBreakdown with string durations.
type JSONSetupBreakdown struct {
	ClientCreation      string `json:"clientCreation" yaml:"clientCreation"`
	TemplateCompilation string `json:"templateCompilation" yaml:"templateCompilation"`
	ConstraintLoading   string `json:"constraintLoading" yaml:"constraintLoading"`
	DataLoading         string `json:"dataLoading" yaml:"dataLoading"`
}

// JSONLatency is a JSON/YAML-friendly version of Latencies with string durations.
type JSONLatency struct {
	Min  string `json:"min" yaml:"min"`
	Max  string `json:"max" yaml:"max"`
	Mean string `json:"mean" yaml:"mean"`
	P50  string `json:"p50" yaml:"p50"`
	P95  string `json:"p95" yaml:"p95"`
	P99  string `json:"p99" yaml:"p99"`
}

// JSONMemoryStats is a JSON/YAML-friendly version of MemoryStats.
type JSONMemoryStats struct {
	AllocsPerReview uint64 `json:"allocsPerReview" yaml:"allocsPerReview"`
	BytesPerReview  string `json:"bytesPerReview" yaml:"bytesPerReview"`
	TotalAllocs     uint64 `json:"totalAllocs" yaml:"totalAllocs"`
	TotalBytes      string `json:"totalBytes" yaml:"totalBytes"`
}

func toJSONResults(results []Results) []JSONResults {
	jsonResults := make([]JSONResults, len(results))
	for i := range results {
		r := &results[i]
		jr := JSONResults{
			Engine:          string(r.Engine),
			TemplateCount:   r.TemplateCount,
			ConstraintCount: r.ConstraintCount,
			ObjectCount:     r.ObjectCount,
			Iterations:      r.Iterations,
			Concurrency:     r.Concurrency,
			TotalReviews:    r.Iterations * r.ObjectCount,
			SetupDuration:   r.SetupDuration.String(),
			SetupBreakdown: JSONSetupBreakdown{
				ClientCreation:      r.SetupBreakdown.ClientCreation.String(),
				TemplateCompilation: r.SetupBreakdown.TemplateCompilation.String(),
				ConstraintLoading:   r.SetupBreakdown.ConstraintLoading.String(),
				DataLoading:         r.SetupBreakdown.DataLoading.String(),
			},
			TotalDuration: r.TotalDuration.String(),
			Latencies: JSONLatency{
				Min:  r.Latencies.Min.String(),
				Max:  r.Latencies.Max.String(),
				Mean: r.Latencies.Mean.String(),
				P50:  r.Latencies.P50.String(),
				P95:  r.Latencies.P95.String(),
				P99:  r.Latencies.P99.String(),
			},
			ViolationCount:     r.ViolationCount,
			ReviewsPerSecond:   r.ReviewsPerSecond,
			SkippedTemplates:   r.SkippedTemplates,
			SkippedConstraints: r.SkippedConstraints,
		}

		// Add memory stats if available
		if r.MemoryStats != nil {
			jr.MemoryStats = &JSONMemoryStats{
				AllocsPerReview: r.MemoryStats.AllocsPerReview,
				BytesPerReview:  formatBytes(r.MemoryStats.BytesPerReview),
				TotalAllocs:     r.MemoryStats.TotalAllocs,
				TotalBytes:      formatBytes(r.MemoryStats.TotalBytes),
			}
		}

		jsonResults[i] = jr
	}
	return jsonResults
}
