#!/bin/bash

controllerpod=$(kubectl -n opa get po --no-headers | awk '{print $1}')
kubectl -n opa exec -it $controllerpod -- sh -c "rm -rf audit && wget http://localhost:7925/v1/audit && base64 -d audit"