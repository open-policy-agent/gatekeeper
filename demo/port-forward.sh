#!/bin/bash

controllerpod=$(kubectl -n opa get po --no-headers | awk '{print $1}')
kubectl -n opa port-forward $controllerpod 7925:7925