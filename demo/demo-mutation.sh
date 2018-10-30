#!/bin/bash

kubectl run nginx --image nginx
kubectl get deployment nginx -o json | jq '.metadata'
kubectl annotate deployment nginx test-mutation=true