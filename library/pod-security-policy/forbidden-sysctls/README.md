# Forbidden Sysctls security context policy

Forbidden sysctls excludes specific sysctls. You can forbid a combination of safe and unsafe sysctls in the list. To forbid setting any sysctls, use `*` on its own. If a sysctl pattern ends with a `*` character, such as `kernel.*`, it'll match `*` with rest of the sysctl.

By default, all safe sysctls are allowed. If you wish to use unsafe sysctls, make sure to whitelist `--allowed-unsafe-sysctls` kubelet flag on each node. For example, `--allowed-unsafe-sysctls='kernel.msg*,kernel.shm.*,net.*'`.
