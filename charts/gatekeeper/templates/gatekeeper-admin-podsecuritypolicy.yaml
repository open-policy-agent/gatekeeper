apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  annotations:
    seccomp.security.alpha.kubernetes.io/allowedProfileNames: '*'
  labels:
    app: '{{ template "gatekeeper.name" . }}'
    chart: '{{ template "gatekeeper.name" . }}'
    gatekeeper.sh/system: "yes"
    heritage: '{{ .Release.Service }}'
    release: '{{ .Release.Name }}'
  name: gatekeeper-admin
spec:
  allowPrivilegeEscalation: false
  fsGroup:
    ranges:
    - max: 65535
      min: 1
    rule: MustRunAs
  requiredDropCapabilities:
  - ALL
  runAsUser:
    rule: MustRunAsNonRoot
  seLinux:
    rule: RunAsAny
  supplementalGroups:
    ranges:
    - max: 65535
      min: 1
    rule: MustRunAs
  volumes:
  - configMap
  - projected
  - secret
  - downwardAPI
