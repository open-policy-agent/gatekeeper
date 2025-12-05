package bench

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestParseOutputFormat(t *testing.T) {
	tests := []struct {
		input   string
		want    OutputFormat
		wantErr bool
	}{
		{"", OutputFormatTable, false},
		{"table", OutputFormatTable, false},
		{"TABLE", OutputFormatTable, false},
		{"json", OutputFormatJSON, false},
		{"JSON", OutputFormatJSON, false},
		{"yaml", OutputFormatYAML, false},
		{"YAML", OutputFormatYAML, false},
		{"invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseOutputFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseOutputFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseOutputFormat(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatResults(t *testing.T) {
	results := []Results{
		{
			Engine:          EngineRego,
			TemplateCount:   2,
			ConstraintCount: 3,
			ObjectCount:     10,
			Iterations:      100,
			SetupDuration:   50 * time.Millisecond,
			TotalDuration:   time.Second,
			Latencies: Latencies{
				Min:  500 * time.Microsecond,
				Max:  5 * time.Millisecond,
				Mean: 1 * time.Millisecond,
				P50:  900 * time.Microsecond,
				P95:  3 * time.Millisecond,
				P99:  4 * time.Millisecond,
			},
			ViolationCount:   50,
			ReviewsPerSecond: 1000,
		},
	}

	t.Run("table format", func(t *testing.T) {
		output, err := FormatResults(results, OutputFormatTable)
		if err != nil {
			t.Fatalf("FormatResults() error = %v", err)
		}

		// Check for expected content
		expectedStrings := []string{
			"REGO Engine",
			"Templates:",
			"Constraints:",
			"Latency",
			"Min:",
			"P99:",
			"Violations Found:",
		}

		for _, s := range expectedStrings {
			if !strings.Contains(output, s) {
				t.Errorf("table output missing expected string %q", s)
			}
		}
	})

	t.Run("json format", func(t *testing.T) {
		output, err := FormatResults(results, OutputFormatJSON)
		if err != nil {
			t.Fatalf("FormatResults() error = %v", err)
		}

		// Check for expected JSON keys
		expectedStrings := []string{
			`"engine": "rego"`,
			`"templateCount": 2`,
			`"constraintCount": 3`,
			`"latencies"`,
			`"min"`,
			`"p99"`,
		}

		for _, s := range expectedStrings {
			if !strings.Contains(output, s) {
				t.Errorf("json output missing expected string %q", s)
			}
		}
	})

	t.Run("yaml format", func(t *testing.T) {
		output, err := FormatResults(results, OutputFormatYAML)
		if err != nil {
			t.Fatalf("FormatResults() error = %v", err)
		}

		// Check for expected YAML keys
		expectedStrings := []string{
			"engine: rego",
			"templateCount: 2",
			"constraintCount: 3",
			"latencies:",
		}

		for _, s := range expectedStrings {
			if !strings.Contains(output, s) {
				t.Errorf("yaml output missing expected string %q", s)
			}
		}
	})
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Nanosecond, "500ns"},
		{1500 * time.Nanosecond, "1.50µs"},
		{500 * time.Microsecond, "500.00µs"},
		{1500 * time.Microsecond, "1.50ms"},
		{500 * time.Millisecond, "500.00ms"},
		{1500 * time.Millisecond, "1.500s"},
		{2 * time.Second, "2.000s"},
	}

	for _, tt := range tests {
		t.Run(tt.d.String(), func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestFormatResults_SetupBreakdown(t *testing.T) {
	results := []Results{
		{
			Engine:          EngineRego,
			TemplateCount:   1,
			ConstraintCount: 1,
			ObjectCount:     1,
			Iterations:      10,
			SetupDuration:   100 * time.Millisecond,
			SetupBreakdown: SetupBreakdown{
				ClientCreation:      10 * time.Millisecond,
				TemplateCompilation: 50 * time.Millisecond,
				ConstraintLoading:   30 * time.Millisecond,
				DataLoading:         10 * time.Millisecond,
			},
			TotalDuration:    time.Second,
			Latencies:        Latencies{Min: time.Millisecond, Max: time.Millisecond, Mean: time.Millisecond},
			ViolationCount:   0,
			ReviewsPerSecond: 10,
		},
	}

	output, err := FormatResults(results, OutputFormatTable)
	if err != nil {
		t.Fatalf("FormatResults() error = %v", err)
	}

	// Check for setup breakdown content
	expectedStrings := []string{
		"Client Creation:",
		"Template Compilation:",
		"Constraint Loading:",
		"Data Loading:",
	}

	for _, s := range expectedStrings {
		if !strings.Contains(output, s) {
			t.Errorf("table output missing setup breakdown: %q", s)
		}
	}
}

func TestFormatResults_SkippedTemplates(t *testing.T) {
	results := []Results{
		{
			Engine:             EngineRego,
			TemplateCount:      2,
			ConstraintCount:    2,
			ObjectCount:        1,
			Iterations:         10,
			SetupDuration:      50 * time.Millisecond,
			TotalDuration:      time.Second,
			Latencies:          Latencies{Min: time.Millisecond, Max: time.Millisecond, Mean: time.Millisecond},
			ViolationCount:     0,
			ReviewsPerSecond:   10,
			SkippedTemplates:   []string{"template1", "template2"},
			SkippedConstraints: []string{"constraint1"},
		},
	}

	output, err := FormatResults(results, OutputFormatTable)
	if err != nil {
		t.Fatalf("FormatResults() error = %v", err)
	}

	// Check for warnings section
	expectedStrings := []string{
		"Warnings:",
		"Skipped Templates:",
		"template1",
		"template2",
		"Skipped Constraints:",
		"constraint1",
	}

	for _, s := range expectedStrings {
		if !strings.Contains(output, s) {
			t.Errorf("table output missing skipped warning: %q", s)
		}
	}
}

func TestFormatResults_ComparisonTable(t *testing.T) {
	results := []Results{
		{
			Engine:           EngineRego,
			TemplateCount:    2,
			ConstraintCount:  2,
			ObjectCount:      10,
			Iterations:       100,
			SetupDuration:    50 * time.Millisecond,
			TotalDuration:    time.Second,
			Latencies:        Latencies{Mean: time.Millisecond, P95: 2 * time.Millisecond, P99: 3 * time.Millisecond},
			ViolationCount:   10,
			ReviewsPerSecond: 1000,
		},
		{
			Engine:           EngineCEL,
			TemplateCount:    2,
			ConstraintCount:  2,
			ObjectCount:      10,
			Iterations:       100,
			SetupDuration:    30 * time.Millisecond,
			TotalDuration:    500 * time.Millisecond,
			Latencies:        Latencies{Mean: 500 * time.Microsecond, P95: time.Millisecond, P99: 2 * time.Millisecond},
			ViolationCount:   10,
			ReviewsPerSecond: 2000,
		},
	}

	output, err := FormatResults(results, OutputFormatTable)
	if err != nil {
		t.Fatalf("FormatResults() error = %v", err)
	}

	// Check for comparison table content
	expectedStrings := []string{
		"Engine Comparison",
		"Metric",
		"REGO",
		"CEL",
		"Throughput",
		"Mean Latency",
		"P95 Latency",
		"P99 Latency",
		"Performance:", // Performance comparison line
	}

	for _, s := range expectedStrings {
		if !strings.Contains(output, s) {
			t.Errorf("table output missing comparison content: %q", s)
		}
	}
}

func TestFormatResults_SetupBreakdownJSON(t *testing.T) {
	results := []Results{
		{
			Engine:          EngineRego,
			TemplateCount:   1,
			ConstraintCount: 1,
			ObjectCount:     1,
			Iterations:      10,
			SetupDuration:   100 * time.Millisecond,
			SetupBreakdown: SetupBreakdown{
				ClientCreation:      10 * time.Millisecond,
				TemplateCompilation: 50 * time.Millisecond,
				ConstraintLoading:   30 * time.Millisecond,
				DataLoading:         10 * time.Millisecond,
			},
			TotalDuration:    time.Second,
			Latencies:        Latencies{Min: time.Millisecond, Max: time.Millisecond, Mean: time.Millisecond},
			ViolationCount:   0,
			ReviewsPerSecond: 10,
		},
	}

	output, err := FormatResults(results, OutputFormatJSON)
	if err != nil {
		t.Fatalf("FormatResults() error = %v", err)
	}

	// Check for setup breakdown in JSON
	expectedStrings := []string{
		`"setupBreakdown"`,
		`"clientCreation"`,
		`"templateCompilation"`,
		`"constraintLoading"`,
		`"dataLoading"`,
	}

	for _, s := range expectedStrings {
		if !strings.Contains(output, s) {
			t.Errorf("json output missing setup breakdown: %q", s)
		}
	}
}

func TestFormatResults_SkippedInJSON(t *testing.T) {
	results := []Results{
		{
			Engine:             EngineRego,
			TemplateCount:      1,
			ConstraintCount:    1,
			ObjectCount:        1,
			Iterations:         10,
			SetupDuration:      50 * time.Millisecond,
			TotalDuration:      time.Second,
			Latencies:          Latencies{Min: time.Millisecond, Max: time.Millisecond, Mean: time.Millisecond},
			ViolationCount:     0,
			ReviewsPerSecond:   10,
			SkippedTemplates:   []string{"skipped-template"},
			SkippedConstraints: []string{"skipped-constraint"},
		},
	}

	output, err := FormatResults(results, OutputFormatJSON)
	if err != nil {
		t.Fatalf("FormatResults() error = %v", err)
	}

	// Check for skipped items in JSON
	expectedStrings := []string{
		`"skippedTemplates"`,
		`"skipped-template"`,
		`"skippedConstraints"`,
		`"skipped-constraint"`,
	}

	for _, s := range expectedStrings {
		if !strings.Contains(output, s) {
			t.Errorf("json output missing skipped items: %q", s)
		}
	}
}

func TestFormatResults_EqualThroughput(t *testing.T) {
	// Test the case where both engines have identical throughput
	results := []Results{
		{
			Engine:           EngineRego,
			TemplateCount:    1,
			ConstraintCount:  1,
			ObjectCount:      1,
			Iterations:       10,
			SetupDuration:    50 * time.Millisecond,
			TotalDuration:    time.Second,
			Latencies:        Latencies{Mean: time.Millisecond, P95: time.Millisecond, P99: time.Millisecond},
			ViolationCount:   0,
			ReviewsPerSecond: 1000, // Same throughput
		},
		{
			Engine:           EngineCEL,
			TemplateCount:    1,
			ConstraintCount:  1,
			ObjectCount:      1,
			Iterations:       10,
			SetupDuration:    50 * time.Millisecond,
			TotalDuration:    time.Second,
			Latencies:        Latencies{Mean: time.Millisecond, P95: time.Millisecond, P99: time.Millisecond},
			ViolationCount:   0,
			ReviewsPerSecond: 1000, // Same throughput
		},
	}

	output, err := FormatResults(results, OutputFormatTable)
	if err != nil {
		t.Fatalf("FormatResults() error = %v", err)
	}

	// Should contain the "similar throughput" message
	if !strings.Contains(output, "similar throughput") {
		t.Error("expected 'similar throughput' message for equal performance")
	}
}

func TestFormatResults_ZeroThroughput(t *testing.T) {
	// Test the case where one engine has zero throughput
	results := []Results{
		{
			Engine:           EngineRego,
			TemplateCount:    1,
			ConstraintCount:  1,
			ObjectCount:      1,
			Iterations:       10,
			SetupDuration:    50 * time.Millisecond,
			TotalDuration:    time.Second,
			Latencies:        Latencies{Mean: time.Millisecond, P95: time.Millisecond, P99: time.Millisecond},
			ViolationCount:   0,
			ReviewsPerSecond: 0, // Zero throughput
		},
		{
			Engine:           EngineCEL,
			TemplateCount:    1,
			ConstraintCount:  1,
			ObjectCount:      1,
			Iterations:       10,
			SetupDuration:    50 * time.Millisecond,
			TotalDuration:    time.Second,
			Latencies:        Latencies{Mean: time.Millisecond, P95: time.Millisecond, P99: time.Millisecond},
			ViolationCount:   0,
			ReviewsPerSecond: 1000,
		},
	}

	output, err := FormatResults(results, OutputFormatTable)
	if err != nil {
		t.Fatalf("FormatResults() error = %v", err)
	}

	// Should NOT contain a performance comparison when one has zero throughput
	if strings.Contains(output, "faster than") {
		t.Error("should not show performance comparison when throughput is zero")
	}
}

func TestFormatResults_RegoFasterThanCEL(t *testing.T) {
	// Test case where Rego is faster than CEL (reversed from normal)
	results := []Results{
		{
			Engine:           EngineRego,
			TemplateCount:    1,
			ConstraintCount:  1,
			ObjectCount:      1,
			Iterations:       10,
			SetupDuration:    50 * time.Millisecond,
			TotalDuration:    time.Second,
			Latencies:        Latencies{Mean: time.Millisecond, P95: time.Millisecond, P99: time.Millisecond},
			ViolationCount:   0,
			ReviewsPerSecond: 2000, // Rego faster
		},
		{
			Engine:           EngineCEL,
			TemplateCount:    1,
			ConstraintCount:  1,
			ObjectCount:      1,
			Iterations:       10,
			SetupDuration:    50 * time.Millisecond,
			TotalDuration:    time.Second,
			Latencies:        Latencies{Mean: time.Millisecond, P95: time.Millisecond, P99: time.Millisecond},
			ViolationCount:   0,
			ReviewsPerSecond: 1000,
		},
	}

	output, err := FormatResults(results, OutputFormatTable)
	if err != nil {
		t.Fatalf("FormatResults() error = %v", err)
	}

	// Should show REGO is faster
	if !strings.Contains(output, "REGO is") || !strings.Contains(output, "faster than CEL") {
		t.Error("expected performance comparison showing REGO faster than CEL")
	}
}

func TestWritePerfDiff_NegativeThroughput(t *testing.T) {
	var buf bytes.Buffer
	r1 := &Results{Engine: EngineRego, ReviewsPerSecond: -1}
	r2 := &Results{Engine: EngineCEL, ReviewsPerSecond: 1000}

	writePerfDiff(&buf, r1, r2)

	// Should not output anything when throughput is negative
	if buf.String() != "" {
		t.Error("expected no output for negative throughput")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes uint64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1572864, "1.50 MB"},
		{1073741824, "1.00 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.bytes)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestFormatResults_WithMemoryStats(t *testing.T) {
	results := []Results{
		{
			Engine:           EngineRego,
			TemplateCount:    1,
			ConstraintCount:  1,
			ObjectCount:      1,
			Iterations:       10,
			SetupDuration:    50 * time.Millisecond,
			TotalDuration:    time.Second,
			Latencies:        Latencies{Min: time.Millisecond, Max: time.Millisecond, Mean: time.Millisecond},
			ViolationCount:   0,
			ReviewsPerSecond: 10,
			MemoryStats: &MemoryStats{
				AllocsPerReview: 500,
				BytesPerReview:  10240,
				TotalAllocs:     5000,
				TotalBytes:      102400,
			},
		},
	}

	t.Run("table format with memory", func(t *testing.T) {
		output, err := FormatResults(results, OutputFormatTable)
		if err != nil {
			t.Fatalf("FormatResults() error = %v", err)
		}

		expectedStrings := []string{
			"Memory:",
			"Allocs/Review:",
			"500",
			"Bytes/Review:",
			"10.00 KB",
			"Total Allocs:",
			"Total Bytes:",
		}

		for _, s := range expectedStrings {
			if !strings.Contains(output, s) {
				t.Errorf("table output missing memory stat: %q", s)
			}
		}
	})

	t.Run("json format with memory", func(t *testing.T) {
		output, err := FormatResults(results, OutputFormatJSON)
		if err != nil {
			t.Fatalf("FormatResults() error = %v", err)
		}

		expectedStrings := []string{
			`"memoryStats"`,
			`"allocsPerReview": 500`,
			`"bytesPerReview": "10.00 KB"`,
			`"totalAllocs": 5000`,
		}

		for _, s := range expectedStrings {
			if !strings.Contains(output, s) {
				t.Errorf("json output missing memory stat: %q", s)
			}
		}
	})
}

func TestFormatResults_ComparisonTableWithMemory(t *testing.T) {
	results := []Results{
		{
			Engine:           EngineRego,
			TemplateCount:    1,
			ConstraintCount:  1,
			ObjectCount:      1,
			Iterations:       10,
			SetupDuration:    50 * time.Millisecond,
			TotalDuration:    time.Second,
			Latencies:        Latencies{Mean: time.Millisecond, P95: time.Millisecond, P99: time.Millisecond},
			ViolationCount:   0,
			ReviewsPerSecond: 1000,
			MemoryStats: &MemoryStats{
				AllocsPerReview: 500,
				BytesPerReview:  10240,
			},
		},
		{
			Engine:           EngineCEL,
			TemplateCount:    1,
			ConstraintCount:  1,
			ObjectCount:      1,
			Iterations:       10,
			SetupDuration:    50 * time.Millisecond,
			TotalDuration:    time.Second,
			Latencies:        Latencies{Mean: time.Millisecond, P95: time.Millisecond, P99: time.Millisecond},
			ViolationCount:   0,
			ReviewsPerSecond: 2000,
			MemoryStats: &MemoryStats{
				AllocsPerReview: 200,
				BytesPerReview:  4096,
			},
		},
	}

	output, err := FormatResults(results, OutputFormatTable)
	if err != nil {
		t.Fatalf("FormatResults() error = %v", err)
	}

	// Check for memory in comparison table
	expectedStrings := []string{
		"Allocs/Review",
		"Bytes/Review",
	}

	for _, s := range expectedStrings {
		if !strings.Contains(output, s) {
			t.Errorf("comparison table missing memory row: %q", s)
		}
	}
}
