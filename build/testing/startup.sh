#!/bin/bash
# this script starts up the docker daemon in order to run a container that acts
# as an OCI image registry. This registry is used in the gator tests, which need
# to push to an image registry in order to test OCI artifact pulling
# functionality
dockerd &> /dev/null &

# block until the daemon starts
timeout=60
until docker version > /dev/null 2>&1
do
  sleep 1
  timeout=$((timeout - 1))
  if [ "$timeout" -lt 1 ]; then
    echo "timeout exceeded waiting for docker daemon"
    exit 1
  fi
done

docker run -v /var/lib/docker -d --rm -p 5000:5000 library/registry:latest

make test-gator
