#!/bin/bash

# assumes port-forward to kube-controller pod
curl -s "http://localhost:7925/v1/audit"  | tr -d '"' | base64 -d 
