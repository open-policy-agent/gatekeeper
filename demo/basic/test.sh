#!/bin/bash

set -eu

# folder we create the code in + cleanup
tmp=$(mktemp -d)
trap "rm -fr $tmp" 0 2 3 15

for yaml in templates/*.yaml
do
  test="${yaml/.yaml/.test.rego}"
  if [ -e $test ]
  then
    # put code (everything after `  rego:`) into nicely named tempfile
    base=$(basename $yaml)
    rego="$tmp/${base/.yaml/.rego}"
    cat $yaml | sed '1,/  rego:/d' > $rego

    # run test
    echo "Running test $test on $rego for $yaml"
    opa test $rego $test --verbose
  fi
done
