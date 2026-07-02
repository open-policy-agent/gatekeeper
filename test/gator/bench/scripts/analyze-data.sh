#!/bin/bash
# Analysis script for gator bench data

OUTPUT_DIR="/tmp/gator-bench-data"

if [ ! -d "$OUTPUT_DIR" ]; then
  echo "Error: No data found. Run gather-data.sh first."
  exit 1
fi

echo "=== Gator Bench Data Analysis ==="
echo ""

###############################################################################
# Test 1: CEL vs Rego Comparison
###############################################################################
echo "=== Test 1: CEL vs Rego Comparison ==="
echo ""

if [ -f "$OUTPUT_DIR/test1_rego.json" ] && [ -f "$OUTPUT_DIR/test1_cel.json" ]; then
  REGO_THROUGHPUT=$(jq -r '.[0].reviewsPerSecond' "$OUTPUT_DIR/test1_rego.json")
  CEL_THROUGHPUT=$(jq -r '.[0].reviewsPerSecond' "$OUTPUT_DIR/test1_cel.json")

  REGO_MEAN=$(jq -r '.[0].latencies.mean' "$OUTPUT_DIR/test1_rego.json")
  CEL_MEAN=$(jq -r '.[0].latencies.mean' "$OUTPUT_DIR/test1_cel.json")

  REGO_P99=$(jq -r '.[0].latencies.p99' "$OUTPUT_DIR/test1_rego.json")
  CEL_P99=$(jq -r '.[0].latencies.p99' "$OUTPUT_DIR/test1_cel.json")

  REGO_SETUP=$(jq -r '.[0].setupDuration' "$OUTPUT_DIR/test1_rego.json")
  CEL_SETUP=$(jq -r '.[0].setupDuration' "$OUTPUT_DIR/test1_cel.json")

  echo "Metric              Rego              CEL               Ratio (CEL/Rego)"
  echo "------              ----              ---               ----------------"
  printf "Throughput          %-17.2f %-17.2f %.2fx\n" "$REGO_THROUGHPUT" "$CEL_THROUGHPUT" "$(echo "scale=2; $CEL_THROUGHPUT / $REGO_THROUGHPUT" | bc)"
  printf "Mean Latency (ns)   %-17.0f %-17.0f %.2fx\n" "$REGO_MEAN" "$CEL_MEAN" "$(echo "scale=2; $REGO_MEAN / $CEL_MEAN" | bc)"
  printf "P99 Latency (ns)    %-17.0f %-17.0f %.2fx\n" "$REGO_P99" "$CEL_P99" "$(echo "scale=2; $REGO_P99 / $CEL_P99" | bc)"
  printf "Setup Time (ns)     %-17.0f %-17.0f %.2fx\n" "$REGO_SETUP" "$CEL_SETUP" "$(echo "scale=2; $REGO_SETUP / $CEL_SETUP" | bc)"
  echo ""
fi

###############################################################################
# Test 2: Concurrency Scaling
###############################################################################
echo "=== Test 2: Concurrency Scaling ==="
echo ""

echo "Concurrency  Throughput     P99 Latency    Efficiency"
echo "-----------  ----------     -----------    ----------"

BASELINE_THROUGHPUT=""
for CONC in 1 2 4 8 16; do
  FILE="$OUTPUT_DIR/test2_conc_${CONC}.json"
  if [ -f "$FILE" ]; then
    THROUGHPUT=$(jq -r '.[0].reviewsPerSecond' "$FILE")
    P99=$(jq -r '.[0].latencies.p99' "$FILE")

    if [ -z "$BASELINE_THROUGHPUT" ]; then
      BASELINE_THROUGHPUT=$THROUGHPUT
      EFFICIENCY="100%"
    else
      # Expected linear scaling
      EXPECTED=$(echo "scale=2; $BASELINE_THROUGHPUT * $CONC" | bc)
      EFF=$(echo "scale=0; ($THROUGHPUT / $EXPECTED) * 100" | bc)
      EFFICIENCY="${EFF}%"
    fi

    P99_MS=$(echo "scale=3; $P99 / 1000000" | bc)
    printf "%-12d %-14.2f %-14.3fms %s\n" "$CONC" "$THROUGHPUT" "$P99_MS" "$EFFICIENCY"
  fi
done
echo ""

###############################################################################
# Test 3: P99 Stability
###############################################################################
echo "=== Test 3: P99 Stability vs Iteration Count ==="
echo ""

echo "Iterations   P50 (µs)    P95 (µs)    P99 (µs)    Mean (µs)"
echo "----------   --------    --------    --------    ---------"

for ITER in 50 100 500 1000 5000; do
  FILE="$OUTPUT_DIR/test3_iter_${ITER}.json"
  if [ -f "$FILE" ]; then
    P50=$(jq -r '.[0].latencies.p50' "$FILE")
    P95=$(jq -r '.[0].latencies.p95' "$FILE")
    P99=$(jq -r '.[0].latencies.p99' "$FILE")
    MEAN=$(jq -r '.[0].latencies.mean' "$FILE")

    P50_US=$(echo "scale=2; $P50 / 1000" | bc)
    P95_US=$(echo "scale=2; $P95 / 1000" | bc)
    P99_US=$(echo "scale=2; $P99 / 1000" | bc)
    MEAN_US=$(echo "scale=2; $MEAN / 1000" | bc)

    printf "%-12d %-11.2f %-11.2f %-11.2f %.2f\n" "$ITER" "$P50_US" "$P95_US" "$P99_US" "$MEAN_US"
  fi
done
echo ""

###############################################################################
# Test 4: Memory Comparison
###############################################################################
echo "=== Test 4: Memory Profiling ==="
echo ""

if [ -f "$OUTPUT_DIR/test4_rego_memory.json" ] && [ -f "$OUTPUT_DIR/test4_cel_memory.json" ]; then
  REGO_ALLOCS=$(jq -r '.[0].memoryStats.allocsPerReview // "N/A"' "$OUTPUT_DIR/test4_rego_memory.json")
  CEL_ALLOCS=$(jq -r '.[0].memoryStats.allocsPerReview // "N/A"' "$OUTPUT_DIR/test4_cel_memory.json")

  REGO_BYTES=$(jq -r '.[0].memoryStats.bytesPerReview // "N/A"' "$OUTPUT_DIR/test4_rego_memory.json")
  CEL_BYTES=$(jq -r '.[0].memoryStats.bytesPerReview // "N/A"' "$OUTPUT_DIR/test4_cel_memory.json")

  echo "Metric              Rego              CEL"
  echo "------              ----              ---"
  printf "Allocs/Review       %-17s %s\n" "$REGO_ALLOCS" "$CEL_ALLOCS"
  printf "Bytes/Review        %-17s %s\n" "$REGO_BYTES" "$CEL_BYTES"
  echo ""
fi

###############################################################################
# Test 5: Warmup Impact
###############################################################################
echo "=== Test 5: Warmup Impact ==="
echo ""

echo "Warmup       Mean (µs)   P99 (µs)"
echo "------       ---------   --------"

for WARMUP in 0 5 10 50 100; do
  FILE="$OUTPUT_DIR/test5_warmup_${WARMUP}.json"
  if [ -f "$FILE" ]; then
    MEAN=$(jq -r '.[0].latencies.mean' "$FILE")
    P99=$(jq -r '.[0].latencies.p99' "$FILE")

    MEAN_US=$(echo "scale=2; $MEAN / 1000" | bc)
    P99_US=$(echo "scale=2; $P99 / 1000" | bc)

    printf "%-12d %-11.2f %.2f\n" "$WARMUP" "$MEAN_US" "$P99_US"
  fi
done
echo ""

###############################################################################
# Test 6: Variance Analysis
###############################################################################
echo "=== Test 6: Variance Analysis ==="
echo ""

echo "Run   Throughput     Mean (µs)    P99 (µs)"
echo "---   ----------     ---------    --------"

SUM_THROUGHPUT=0
SUM_MEAN=0
SUM_P99=0
COUNT=0

for RUN in 1 2 3 4 5; do
  FILE="$OUTPUT_DIR/test6_run_${RUN}.json"
  if [ -f "$FILE" ]; then
    THROUGHPUT=$(jq -r '.[0].reviewsPerSecond' "$FILE")
    MEAN=$(jq -r '.[0].latencies.mean' "$FILE")
    P99=$(jq -r '.[0].latencies.p99' "$FILE")

    MEAN_US=$(echo "scale=2; $MEAN / 1000" | bc)
    P99_US=$(echo "scale=2; $P99 / 1000" | bc)

    printf "%-5d %-14.2f %-12.2f %.2f\n" "$RUN" "$THROUGHPUT" "$MEAN_US" "$P99_US"

    SUM_THROUGHPUT=$(echo "$SUM_THROUGHPUT + $THROUGHPUT" | bc)
    SUM_MEAN=$(echo "$SUM_MEAN + $MEAN_US" | bc)
    SUM_P99=$(echo "$SUM_P99 + $P99_US" | bc)
    COUNT=$((COUNT + 1))
  fi
done

if [ $COUNT -gt 0 ]; then
  AVG_THROUGHPUT=$(echo "scale=2; $SUM_THROUGHPUT / $COUNT" | bc)
  AVG_MEAN=$(echo "scale=2; $SUM_MEAN / $COUNT" | bc)
  AVG_P99=$(echo "scale=2; $SUM_P99 / $COUNT" | bc)

  echo "---   ----------     ---------    --------"
  printf "AVG   %-14.2f %-12.2f %.2f\n" "$AVG_THROUGHPUT" "$AVG_MEAN" "$AVG_P99"
fi
echo ""

echo "=== Analysis Complete ==="
