#!/bin/bash

echo "Delete all will delete all kubebernetes-policy-controller components"

read -p "Press enter to continue"

rm -rf ./secret

./delete-webhook.sh
kubectl delete ns gatekeeper-system
