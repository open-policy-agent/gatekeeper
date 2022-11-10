#!/bin/bash
dockerd &> /dev/null &

# block until the daemon starts
until docker version > /dev/null 2>&1
do
  sleep 1
done

docker run -v /var/lib/docker -d --rm -p 5000:5000 library/registry:latest

make test-gator
