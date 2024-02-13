> [!WARNING]
> This is a demo of a prototype-stage feature and is subject to change.

## Pre-requisites

- Requires minimum Gatekeeper v3.14.0
- Set `--experimental-enable-k8s-native-validation` in Gatekeeper deployments.
- Set `--validate-template-rego=false` in Gatekeeper deployments if using Gatekeeper version 3.14.0 and later. This flag will be removed in v3.16.0, and will not be applicable in the future.

## Demo

<img width= "900" height="500" src="demo.gif" alt="cel demo">