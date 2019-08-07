# ProcMount security context policy

`procMount` denotes the type of proc mount to use for the containers. The default is `DefaultProcMount` which uses the container runtime defaults for readonly paths and masked paths.

Types of proc mount are:

- `DefaultProcMount` uses the container runtime default ProcType. Most container runtimes mask certain paths in /proc to avoid accidental security exposure of special devices or information.

- `UnmaskedProcMount` bypasses the default masking behavior of the container runtime and ensures the newly created /proc the container stays in tact with no modifications.

This requires the `ProcMountType` feature flag to be enabled. Set `--feature-gates=ProcMountType=true` in Kubernetes API Server to be able to use `Unmasked` procMount type (requires v1.12 and above). For more information, see
https://kubernetes.io/docs/reference/command-line-tools-reference/kube-apiserver/#options and https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/.
