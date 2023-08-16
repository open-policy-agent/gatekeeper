This file holds a fork of the Kubernetes CEL compiler code.

It is needed because there is no way to add arbitrary new CEL environment variables
the way the K8s CEL library is structured in 1.27. Because `variables` did not exist in
k8s 1.27, this means we would be unable to use that feature without this fork.

Maintaining a fork will be unnecessary as of K8s v1.28.