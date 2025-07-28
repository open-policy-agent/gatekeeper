---
id: input
title: Admission Review Input
---

The data that's passed to Gatekeeper for review is in the form of an `input.review` object that stores the [admission request](https://pkg.go.dev/k8s.io/kubernetes/pkg/apis/admission#AdmissionRequest) under evaluation. It follows a structure that contains the object being created, and in the case of update operations the old object being updated. It has the following fields:
- `dryRun`: Describes if the request was invoked by `kubectl --dry-run`. This cannot be populated by Kubernetes for audit.
- `kind`: The resource `kind`, `group`, `version` of the request object under evaluation.
- `name`: The name of the request object under evaluation. It may be empty if the deployment expects the API server to generate a name for the requested resource.
- `namespace`: The namespace of the request object under evaluation. Empty for cluster scoped objects.
- `object`: The request object under evaluation to be created or modified.
- `oldObject`: The original state of the request object under evaluation. This is only available for UPDATE operations.
- `operation`: The operation for the request (e.g. CREATE, UPDATE). This cannot be populated by Kubernetes for audit.
- `uid`: The request's unique identifier. This cannot be populated by Kubernetes for audit.
- `userInfo`: The request's user's information such as `username`, `uid`, `groups`, `extra`. This cannot be populated by Kubernetes for audit.

> **_NOTE_** For `input.review` fields above that cannot be populated by Kubernetes for audit reviews, the constraint templates that rely on them are not auditable. It is up to the rego author to handle the case where these fields are unset and empty in order to avoid every matching resource being reported as violating resources. 

You can see an example of the request structure below.

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