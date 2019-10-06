#!/bin/bash

tag="$1"
if [ -z "$tag" ] ; then
    echo "Usage: $0 <image tag>" >&2
    exit 1
fi
version="${tag}"

CHART_PATH=chart/gatekeeper-operator

echo "Updating chart to version to: ${version}"
sed -i.bak -E "
    s#version: .*#version: ${version}#
    s#appVersion: .*#appVersion: ${tag}#
" ${CHART_PATH}/Chart.yaml
rm ${CHART_PATH}/Chart.yaml.bak

echo "Updating chart images tag to: ${version}"
sed -i.bak -E "
    s#image: (.*):.*#image: \\1:${version}#
    s#sidecarImage: (.*):.*#sidecarImage: \\1:${version}#
    s#  image: (.*): (.*):.*#  image: \\1:${version}#
" ${CHART_PATH}/values.yaml
rm ${CHART_PATH}/values.yaml.bak
