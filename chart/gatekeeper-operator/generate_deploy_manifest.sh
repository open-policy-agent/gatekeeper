#!/bin/bash
scriptdir="$(dirname "$0")"
cd "$scriptdir"
deploymanifest="../../deploy.yaml"
# Create gatekeeper-system namespace
cat example_namespace.yaml > $deploymanifest
# Write out templated chart
helm template -n gatekeeper-system --name-template gatekeeper-operator . >> $deploymanifest
# Remove Helm references
sed -i -n '/helm.sh\/chart:/!p' $deploymanifest
sed -i -n '/app.kubernetes.io\/managed-by: Helm/!p' $deploymanifest
