#!/bin/bash

controllerpod=$(kubectl -n gatekeeper-system get po --no-headers | awk '{print $1}')
kubectl -n gatekeeper-system port-forward $controllerpod 7925:7925