#!/bin/bash
dockerd &> dockerd-logfile &

# block until the daemon starts
until docker version > /dev/null 2>&1
do
  sleep 1
done

docker run -d --rm -p 5000:5000 registry

make test-gator