---
id: input
title: Admission Review Input
---

The data that's passed to Gatekeeper for review follows a structure that contains the object being created, and in the case of update operations the old object being updated. You can see an example of the request structure below.

```json
{
  "apiVersion": "admission.k8s.io/v1",
  "kind": "AdmissionReview",
  "request": {
    "uid": "abc123",
    "kind": {
      "group": "apps",
      "version": "v1",
      "kind": "Deployment"
    },
    "resource": {
      "group": "apps",
      "version": "v1",
      "resource": "deployments"
    },
    "namespace": "default",
    "operation": "CREATE",
    "userInfo": {
      "username": "john_doe",
      "groups": ["developers"]
    },
    "object": {
      // The resource object being created, updated, or deleted
      "metadata": {
        "name": "my-deployment",
        "labels": {
          "app": "my-app",
          "env": "production"
        }
      },
      "spec": {
        // Specific configuration for the resource
        "replicas": 3,
        // ... other fields ...
      }
    },
    "oldObject": {
      // For update requests, the previous state of the resource
      "metadata": {
        "name": "my-deployment",
        "labels": {
          "app": "my-app",
          "env": "staging"
        }
      },
      "spec": {
        // Previous configuration for the resource
        "replicas": 2,
        // ... other fields ...
      }
    }
  }
}
```