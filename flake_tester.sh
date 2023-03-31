#!/bin/bash
n=$1
echo "running pkg/controller tests $n times"
touch tmp.txt
for i in $(seq 1 $n)
do
  go test ./pkg/controller/... -v -count=1 > tmp.txt
  retVal=$?
  if [ $retVal -ne 0 ]; then
    echo "Failure at run ${i}"
    echo "--- output ---"
    cat tmp.txt
    echo "--------------"
    exit 1
  else
    echo "Run ${i} passed"
  fi
done
