#!/bin/bash
set -e

for path in $PWD/*; do
    if [ -d $path ]
    then
        echo $path
        cd $path
        docker run -v $path:/tests openpolicyagent/opa test /tests/src.rego /tests/src_test.rego
    fi
done
