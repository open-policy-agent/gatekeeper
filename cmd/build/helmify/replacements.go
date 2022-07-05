package main

var replacements = map[string]string{
	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_CONTAINER_RESOURCES: ""`: `{{- toYaml .Values.controllerManager.resources | nindent 10 }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_CONTAINER_RESOURCES: ""`: `{{- toYaml .Values.audit.resources | nindent 10 }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_HOST_NETWORK": `{{ .Values.controllerManager.hostNetwork }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_DNS_POLICY": `{{ .Values.controllerManager.dnsPolicy }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_PORT": `{{ .Values.controllerManager.port }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_HEALTH_PORT": `{{ .Values.controllerManager.healthPort }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_METRICS_PORT": `{{ .Values.controllerManager.metricsPort }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_HOST_NETWORK": `{{ .Values.audit.hostNetwork }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_DNS_POLICY": `{{ .Values.audit.dnsPolicy }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_HEALTH_PORT": `{{ .Values.audit.healthPort }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_METRICS_PORT": `{{ .Values.audit.metricsPort }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_NODE_SELECTOR: ""`: `{{- toYaml .Values.audit.nodeSelector | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_AFFINITY: ""`: `{{- toYaml .Values.audit.affinity | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_SECURITY_CONTEXT: ""`: `{{- if .Values.enableRuntimeDefaultSeccompProfile }}
          seccompProfile:
            type: RuntimeDefault
          {{- end }}
          {{- toYaml .Values.audit.securityContext | nindent 10}}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_TOLERATIONS: ""`: `{{- toYaml .Values.audit.tolerations | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_IMAGE_PULL_SECRETS: ""`: `{{- toYaml .Values.image.pullSecrets | nindent 8 }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_PRIORITY_CLASS_NAME": `{{ .Values.audit.priorityClassName }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_NODE_SELECTOR: ""`: `{{- toYaml .Values.controllerManager.nodeSelector | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_AFFINITY: ""`: `{{- toYaml .Values.controllerManager.affinity | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_SECURITY_CONTEXT: ""`: `{{- if .Values.enableRuntimeDefaultSeccompProfile }}
          seccompProfile:
            type: RuntimeDefault
          {{- end }}
          {{- toYaml .Values.controllerManager.securityContext | nindent 10}}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_TOLERATIONS: ""`: `{{- toYaml .Values.controllerManager.tolerations | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_IMAGE_PULL_SECRETS: ""`: `{{- toYaml .Values.image.pullSecrets | nindent 8 }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_PRIORITY_CLASS_NAME": `{{ .Values.controllerManager.priorityClassName }}`,

	"HELMSUBST_DEPLOYMENT_REPLICAS": `{{ .Values.replicas }}`,

	`HELMSUBST_ANNOTATIONS: ""`: `{{- if .Values.podAnnotations }}
        {{- toYaml .Values.podAnnotations | trim | nindent 8 }}
        {{- end }}`,

	"HELMSUBST_SECRET_ANNOTATIONS": `{{- toYaml .Values.secretAnnotations | trim | nindent 4 }}`,

	"- HELMSUBST_TLS_HEALTHCHECK_ENABLED_ARG": `{{ if .Values.enableTLSHealthcheck}}- --enable-tls-healthcheck{{- end }}`,

	"- HELMSUBST_MUTATION_ENABLED_ARG": `{{ if not .Values.disableMutation}}- --operation=mutation-webhook{{- end }}`,

	"- HELMSUBST_MUTATION_STATUS_ENABLED_ARG": `{{ if not .Values.disableMutation}}- --operation=mutation-status{{- end }}`,

	"HELMSUBST_MUTATING_WEBHOOK_FAILURE_POLICY": `{{ .Values.mutatingWebhookFailurePolicy }}`,

	"HELMSUBST_MUTATING_WEBHOOK_REINVOCATION_POLICY": `{{ .Values.mutatingWebhookReinvocationPolicy }}`,

	"- HELMSUBST_MUTATING_WEBHOOK_EXEMPT_NAMESPACE_LABELS": `
    {{- range $key, $value := .Values.mutatingWebhookExemptNamespacesLabels}}
    - key: {{ $key }}
      operator: NotIn
      values:
      {{- range $value }}
      - {{ . }}
      {{- end }}
    {{- end }}`,

	"HELMSUBST_MUTATING_WEBHOOK_OBJECT_SELECTOR": `{{ toYaml .Values.mutatingWebhookObjectSelector }}`,

	"HELMSUBST_MUTATING_WEBHOOK_TIMEOUT": `{{ .Values.mutatingWebhookTimeoutSeconds }}`,
	"- HELMSUBST_MUTATING_WEBHOOK_OPERATION_RULES": `{{- if .Values.mutatingWebhookCustomRules }}
  {{- toYaml .Values.mutatingWebhookCustomRules | nindent 2 }}
  {{- else }}
  - apiGroups:
    - '*'
    apiVersions:
    - '*'
    operations:
    - CREATE
    - UPDATE
    resources:
    - '*'
  {{- end }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_TIMEOUT": `{{ .Values.validatingWebhookTimeoutSeconds }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_FAILURE_POLICY": `{{ .Values.validatingWebhookFailurePolicy }}`,

	"- HELMSUBST_VALIDATING_WEBHOOK_EXEMPT_NAMESPACE_LABELS": `
    {{- range $key, $value := .Values.validatingWebhookExemptNamespacesLabels}}
    - key: {{ $key }}
      operator: NotIn
      values:
      {{- range $value }}
      - {{ . }}
      {{- end }}
    {{- end }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_OBJECT_SELECTOR": `{{ toYaml .Values.validatingWebhookObjectSelector }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_CHECK_IGNORE_FAILURE_POLICY": `{{ .Values.validatingWebhookCheckIgnoreFailurePolicy }}`,

	"HELMSUBST_RESOURCEQUOTA_POD_LIMIT": `{{ .Values.podCountLimit }}`,

	"- HELMSUBST_VALIDATING_WEBHOOK_OPERATION_RULES": `{{- if .Values.validatingWebhookCustomRules }}
  {{- toYaml .Values.validatingWebhookCustomRules | nindent 2 }}
  {{- else }}
  - apiGroups:
    - '*'
    apiVersions:
    - '*'
    operations:
    - CREATE
    - UPDATE
    {{- if .Values.enableDeleteOperations }}
    - DELETE
    {{- end }}
    resources:
    - '*'
    # Explicitly list all known subresources except "status" (to avoid destabilizing the cluster and increasing load on gatekeeper).
    # You can find a rough list of subresources by doing a case-sensitive search in the Kubernetes codebase for 'Subresource("'
    - 'pods/ephemeralcontainers'
    - 'pods/exec'
    - 'pods/log'
    - 'pods/eviction'
    - 'pods/portforward'
    - 'pods/proxy'
    - 'pods/attach'
    - 'pods/binding'
    - 'deployments/scale'
    - 'replicasets/scale'
    - 'statefulsets/scale'
    - 'replicationcontrollers/scale'
    - 'services/proxy'
    - 'nodes/proxy'
    # For constraints that mitigate CVE-2020-8554
    - 'services/status'
  {{- end }}`,

	"HELMSUBST_PDB_CONTROLLER_MANAGER_MINAVAILABLE": `{{ .Values.pdb.controllerManager.minAvailable }}`,

	`HELMSUBST_AUDIT_CONTROLLER_MANAGER_DEPLOYMENT_IMAGE_RELEASE: ""`: `{{- if .Values.image.release }}
        image: {{ .Values.image.repository }}:{{ .Values.image.release }}
        {{- else }}
        image: {{ .Values.image.repository }}
        {{- end }}`,

	`HELMSUBST_SERVICE_TYPE: ""`: `{{- if .Values.service }}
  type: {{ .Values.service.type | default "ClusterIP" }}
    {{- if .Values.service.loadBalancerIP }}
  loadBalancerIP: {{ .Values.service.loadBalancerIP }}
    {{- end }}
  {{- end }}`,

	`HELMSUBST_SERVICE_HEALTHZ: ""`: `
  ports:
  - name: https-webhook-server
    port: 443
    targetPort: webhook-server
{{- if .Values.service }}
{{- if .Values.service.healthzPort }}
  - name: http-webhook-healthz
    port: {{ .Values.service.healthzPort }}
    targetPort: healthz
      {{- end }}
  {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_DISABLED_BUILTIN": `
        {{- range .Values.disabledBuiltins}}
        - --disable-opa-builtin={{ . }}
        {{- end }}`,
	"- HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_EXEMPT_NAMESPACES": `
        {{- range .Values.controllerManager.exemptNamespaces}}
        - --exempt-namespace={{ . }}
        {{- end }}`,

	"- HELMSUBST_METRICS_BACKEND_ARG": `
        {{- range .Values.metricsBackends}}
        - --metrics-backend={{ . }}
        {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_EXEMPT_NAMESPACE_PREFIXES": `
        {{- range .Values.controllerManager.exemptNamespacePrefixes}}
        - --exempt-namespace-prefix={{ . }}
        {{- end }}`,
}
