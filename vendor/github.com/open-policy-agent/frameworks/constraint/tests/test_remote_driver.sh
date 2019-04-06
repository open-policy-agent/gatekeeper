#!/usr/bin/env bash

opa run --server --addr localhost:8181 -l debug &
opa_proc=$!

sleep 1

clean() {
  kill -9 $opa_proc
}

trap clean EXIT

go test ./tests
