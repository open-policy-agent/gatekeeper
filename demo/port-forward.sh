#!/bin/bash

controllerpod=$(kubectl -n kpc-system get po --no-headers | awk '{print $1}')
kubectl -n kpc-system port-forward $controllerpod 7925:7925