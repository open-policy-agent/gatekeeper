#!/bin/bash
# Performance data gathering script for gator bench
# This script collects data to understand performance characteristics

set -e

GATOR="./bin/gator"
OUTPUT_DIR="/tmp/gator-bench-data"
ITERATIONS=1000

mkdir -p "$OUTPUT_DIR"

echo "=== Gator Bench Data Collection ==="
echo "Output directory: $OUTPUT_DIR"
echo "Iterations per test: $ITERATIONS"
echo ""

# Build gator first
echo "Building gator..."
make gator > /dev/null 2>&1
echo "Done."
echo ""

###############################################################################
# Test 1: CEL vs Rego - Same Policy (K8sAllowedRepos supports both)
###############################################################################
echo "=== Test 1: CEL vs Rego Comparison ==="

echo "Running Rego engine..."
$GATOR bench \
  --filename test/gator/bench/both/ \
  --engine rego \
  --iterations $ITERATIONS \
  --output json > "$OUTPUT_DIR/test1_rego.json"

echo "Running CEL engine..."
$GATOR bench \
  --filename test/gator/bench/both/ \
  --engine cel \
  --iterations $ITERATIONS \
  --output json > "$OUTPUT_DIR/test1_cel.json"

echo "Results saved to test1_rego.json and test1_cel.json"
echo ""

###############################################################################
# Test 2: Concurrency Scaling
###############################################################################
echo "=== Test 2: Concurrency Scaling ==="

for CONC in 1 2 4 8 16; do
  echo "Running with concurrency=$CONC..."
  $GATOR bench \
    --filename test/gator/bench/basic/ \
    --iterations $ITERATIONS \
    --concurrency $CONC \
    --output json > "$OUTPUT_DIR/test2_conc_${CONC}.json"
done

echo "Results saved to test2_conc_*.json"
echo ""

###############################################################################
# Test 3: Iteration Count Impact on P99 Stability
###############################################################################
echo "=== Test 3: P99 Stability vs Iteration Count ==="

for ITER in 50 100 500 1000 5000; do
  echo "Running with iterations=$ITER..."
  $GATOR bench \
    --filename test/gator/bench/basic/ \
    --iterations $ITER \
    --output json > "$OUTPUT_DIR/test3_iter_${ITER}.json"
done

echo "Results saved to test3_iter_*.json"
echo ""

###############################################################################
# Test 4: Memory Profiling Comparison
###############################################################################
echo "=== Test 4: Memory Profiling ==="

echo "Running Rego with memory profiling..."
$GATOR bench \
  --filename test/gator/bench/both/ \
  --engine rego \
  --iterations $ITERATIONS \
  --memory \
  --output json > "$OUTPUT_DIR/test4_rego_memory.json"

echo "Running CEL with memory profiling..."
$GATOR bench \
  --filename test/gator/bench/both/ \
  --engine cel \
  --iterations $ITERATIONS \
  --memory \
  --output json > "$OUTPUT_DIR/test4_cel_memory.json"

echo "Results saved to test4_*_memory.json"
echo ""

###############################################################################
# Test 5: Warmup Impact
###############################################################################
echo "=== Test 5: Warmup Impact ==="

for WARMUP in 0 5 10 50 100; do
  echo "Running with warmup=$WARMUP..."
  $GATOR bench \
    --filename test/gator/bench/basic/ \
    --iterations 500 \
    --warmup $WARMUP \
    --output json > "$OUTPUT_DIR/test5_warmup_${WARMUP}.json"
done

echo "Results saved to test5_warmup_*.json"
echo ""

###############################################################################
# Test 6: Multiple Runs for Variance Analysis
###############################################################################
echo "=== Test 6: Variance Analysis (5 runs) ==="

for RUN in 1 2 3 4 5; do
  echo "Run $RUN/5..."
  $GATOR bench \
    --filename test/gator/bench/basic/ \
    --iterations $ITERATIONS \
    --output json > "$OUTPUT_DIR/test6_run_${RUN}.json"
done

echo "Results saved to test6_run_*.json"
echo ""

###############################################################################
# Summary
###############################################################################
echo "=== Data Collection Complete ==="
echo ""
echo "All data saved to: $OUTPUT_DIR"
echo ""
echo "To analyze, run: ./test/gator/bench/analyze-data.sh"
