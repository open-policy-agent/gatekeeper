package main

var replacements = map[string]string{
	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_CONTAINER_RESOURCES: ""`: `{{- toYaml .Values.controllerManager.resources | nindent 10 }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_CONTAINER_RESOURCES: ""`: `{{- toYaml .Values.audit.resources | nindent 10 }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_HOST_NETWORK": `{{ .Values.controllerManager.hostNetwork }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_DNS_POLICY": `{{ .Values.controllerManager.dnsPolicy }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_PORT": `{{ .Values.controllerManager.port }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_HEALTH_PORT": `{{ .Values.controllerManager.healthPort }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_METRICS_PORT": `{{ .Values.controllerManager.metricsPort }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_READINESS_TIMEOUT": `{{ .Values.controllerManager.readinessTimeout }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_LIVENESS_TIMEOUT": `{{ .Values.controllerManager.livenessTimeout }}`,

	"- HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_EMIT_ADMISSION_EVENTS": `{{ if hasKey .Values "emitAdmissionEvents" }}- --emit-admission-events={{ .Values.emitAdmissionEvents }}{{- end }}`,

	"- HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_LOG_STATS_ADMISSION": `{{ if hasKey .Values "logStatsAdmission" }}- --log-stats-admission={{ .Values.logStatsAdmission }}{{- end }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_HOST_NETWORK": `{{ .Values.audit.hostNetwork }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_DNS_POLICY": `{{ .Values.audit.dnsPolicy }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_SERVICE_ACCOUNT_NAME": `{{ .Values.controllerManager.serviceAccount.name }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_SERVICE_ACCOUNT_AUTOMOUNT_TOKEN": `{{ .Values.controllerManager.serviceAccount.automountServiceAccountToken }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_SERVICE_ACCOUNT_NAME": `{{ .Values.audit.serviceAccount.name }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_SERVICE_ACCOUNT_AUTOMOUNT_TOKEN": `{{ .Values.audit.serviceAccount.automountServiceAccountToken }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_HEALTH_PORT": `{{ .Values.audit.healthPort }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_METRICS_PORT": `{{ .Values.audit.metricsPort }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_READINESS_TIMEOUT": `{{ .Values.audit.readinessTimeout }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_LIVENESS_TIMEOUT": `{{ .Values.audit.livenessTimeout }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_NODE_SELECTOR: ""`: `{{- toYaml .Values.audit.nodeSelector | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_POD_SECURITY_CONTEXT: ""`: `{{- toYaml .Values.audit.podSecurityContext | nindent 8 }}`,

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

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_POD_SECURITY_CONTEXT: ""`: `{{- toYaml .Values.controllerManager.podSecurityContext | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_AFFINITY: ""`: `{{- toYaml .Values.controllerManager.affinity | nindent 8 }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_STRATEGY_TYPE": `{{ .Values.controllerManager.strategyType }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_STRATEGY_ROLLINGUPDATE: ""`: `{{- if .Values.controllerManager.strategyRollingUpdate }}
    rollingUpdate:
    {{- toYaml .Values.controllerManager.strategyRollingUpdate | nindent 6 }}
    {{- end }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_SECURITY_CONTEXT: ""`: `{{- if .Values.enableRuntimeDefaultSeccompProfile }}
          seccompProfile:
            type: RuntimeDefault
          {{- end }}
          {{- toYaml .Values.controllerManager.securityContext | nindent 10}}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_TOLERATIONS: ""`: `{{- toYaml .Values.controllerManager.tolerations | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_TOPOLOGY_SPREAD_CONSTRAINTS: ""`: `{{- toYaml .Values.controllerManager.topologySpreadConstraints | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_IMAGE_PULL_SECRETS: ""`: `{{- toYaml .Values.image.pullSecrets | nindent 8 }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_PRIORITY_CLASS_NAME": `{{ .Values.controllerManager.priorityClassName }}`,

	"HELMSUBST_DEPLOYMENT_REPLICAS": `{{ .Values.replicas }}`,

	`HELMSUBST_DEPLOYMENT_LABELS: ""`: `{{- include "gatekeeper.commonLabels" . | nindent 4 }}`,

	"HELMSUBST_DEPLOYMENT_REVISION_HISTORY_LIMIT": `{{ .Values.revisionHistoryLimit }}`,

	`HELMSUBST_DEPLOYMENT_ANNOTATIONS: ""`: `{{- include "gatekeeper.commonAnnotations" . | nindent 4 }}`,

	`HELMSUBST_ANNOTATIONS: ""`: `{{- if .Values.podAnnotations }}
        {{- toYaml .Values.podAnnotations | trim | nindent 8 }}
        {{- end }}`,

	`HELMSUBST_AUDIT_POD_ANNOTATIONS: ""`: `{{- if .Values.auditPodAnnotations }}
        {{- toYaml .Values.auditPodAnnotations | trim | nindent 8 }}
        {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_AUDIT_CHUNK_SIZE": `{{ if hasKey .Values "auditChunkSize" }}- --audit-chunk-size={{ .Values.auditChunkSize }}{{- end }}`,

	"- HELMSUBST_DEPLOYMENT_AUDIT_EMIT_EVENTS": `{{ if hasKey .Values "emitAuditEvents" }}- --emit-audit-events={{ .Values.emitAuditEvents }}{{- end }}`,

	"- HELMSUBST_DEPLOYMENT_AUDIT_LOG_STATS_ADMISSION": `{{ if hasKey .Values "logStatsAudit" }}- --log-stats-audit={{ .Values.logStatsAudit }}{{- end }}`,

	"HELMSUBST_SECRET_ANNOTATIONS": `{{- toYaml .Values.secretAnnotations | trim | nindent 4 }}`,

	"- HELMSUBST_TLS_HEALTHCHECK_ENABLED_ARG": `{{ if .Values.enableTLSHealthcheck}}- --enable-tls-healthcheck{{- end }}`,

	"- HELMSUBST_ADDITIONAL_VALIDATING_WEBHOOK_CONFIGS_TO_ROTATE_CERTS": `{{ if .Values.additionalValidatingWebhookConfigsToRotateCerts | empty | not }}- --additional-validating-webhook-configs-to-rotate-certs={{ .Values.additionalValidatingWebhookConfigsToRotateCerts | join "," }}{{- end }}`,

	"- HELMSUBST_ADDITIONAL_MUTATING_WEBHOOK_CONFIGS_TO_ROTATE_CERTS": `{{ if .Values.additionalMutatingWebhookConfigsToRotateCerts | empty | not }}- --additional-mutating-webhook-configs-to-rotate-certs={{ .Values.additionalMutatingWebhookConfigsToRotateCerts | join "," }}{{- end }}`,

	"- HELMBUST_ENABLE_TLS_APISERVER_AUTHENTICATION": `{{ if ne .Values.controllerManager.clientCertName "" }}- --client-cert-name={{ .Values.controllerManager.clientCertName }}{{- end }}`,

	"- HELMSUBST_MUTATION_ENABLED_ARG": `{{ if not .Values.disableMutation}}- --operation=mutation-webhook{{- end }}`,

	"- HELMSUBST_MUTATION_STATUS_ENABLED_ARG": `{{ if not .Values.disableMutation}}- --operation=mutation-status{{- end }}`,

	"- HELMSUBST_CONTROLLER_MANAGER_OPERATIONS": `
        {{- if not .Values.controllerManager.disableWebhookOperation }}
        - --operation=webhook
        {{- end }}
        {{- if not .Values.controllerManager.disableGenerateOperation }}
        - --operation=generate
        {{- end }}`,

	"- HELMSUBST_AUDIT_OPERATIONS": `
        {{- if not .Values.audit.disableGenerateOperation }}
        - --operation=generate
        {{- end }}
        {{- if not .Values.audit.disableAuditOperation }}
        - --operation=audit
        {{- end }}
        {{- if not .Values.audit.disableStatusOperation }}
        - --operation=status
        {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_AUDIT_VIOLATION_EXPORT_ARGS": `{{ if hasKey .Values "enableViolationExport" }}
        - --enable-violation-export={{ .Values.enableViolationExport }}
        {{- end }}
        {{ if hasKey .Values.audit "connection" }}
        - --audit-connection={{ .Values.audit.connection }}
        {{- end }}
        {{ if hasKey .Values.audit "channel" }}
        - --audit-channel={{ .Values.audit.channel }}
        {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_SYNC_VAP_ENFORCEMENT_SCOPE": `{{ if hasKey .Values "syncVAPEnforcementScope" }}- --sync-vap-enforcement-scope={{ .Values.syncVAPEnforcementScope }}{{- end }}`,

	"HELMSUBST_MUTATING_WEBHOOK_FAILURE_POLICY": `{{ .Values.mutatingWebhookFailurePolicy }}`,

	"HELMSUBST_MUTATING_WEBHOOK_REINVOCATION_POLICY": `{{ .Values.mutatingWebhookReinvocationPolicy }}`,

	"HELMSUBST_MUTATING_WEBHOOK_ANNOTATIONS": `{{- toYaml .Values.mutatingWebhookAnnotations | trim | nindent 4 }}`,

	"- HELMSUBST_MUTATING_WEBHOOK_EXEMPT_NAMESPACE_LABELS": `
    {{- /* 1. Get mandatory exemption from helper */ -}}
    {{- $defaults := include "gatekeeper.mandatoryNamespaceExemption" . | fromYaml -}}
    {{- /* 2. Merge user values with mandatory exemption. */ -}}
    {{- $merged := merge (deepCopy .Values.mutatingWebhookExemptNamespacesLabels) $defaults -}}
    {{- range $key, $value := $merged }}
    - key: {{ $key }}
      operator: NotIn
      values:
      {{- /* Ensure current namespace is in the list for the metadata key */ -}}
      {{- $list := $value -}}
      {{- if eq $key "kubernetes.io/metadata.name" }}
        {{- $list = append $value $.Release.Namespace | uniq -}}
      {{- end }}
      {{- range $list }}
      - {{ . }}
      {{- end }}
    {{- end }}`,

	"HELMSUBST_MUTATING_WEBHOOK_OBJECT_SELECTOR": `{{ toYaml .Values.mutatingWebhookObjectSelector | nindent 4 }}`,

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
    {{- range .Values.mutatingWebhookSubResources }}
    - {{ . }}
    {{- end }}
    scope: '{{ .Values.mutatingWebhookScope }}'
  {{- end }}`,

	"HELMSUBST_MUTATING_WEBHOOK_CLIENT_CONFIG: \"\"": `{{- if .Values.mutatingWebhookURL }}
    url: https://{{ .Values.mutatingWebhookURL }}/v1/mutate
    {{- else }}
    service:
      name: gatekeeper-webhook-service
      namespace: '{{ .Release.Namespace }}'
      path: /v1/mutate
    {{- end }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_MATCH_CONDITIONS": `{{ toYaml .Values.validatingWebhookMatchConditions | nindent 4 }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_TIMEOUT": `{{ .Values.validatingWebhookTimeoutSeconds }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_FAILURE_POLICY": `{{ .Values.validatingWebhookFailurePolicy }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_ANNOTATIONS": `{{- toYaml .Values.validatingWebhookAnnotations | trim | nindent 4 }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_MATCHEXPRESSION_METADATANAME": `key: kubernetes.io/metadata.name
      operator: NotIn
      values:
      - {{ .Release.Namespace }}`,

	"- HELMSUBST_VALIDATING_WEBHOOK_EXEMPT_NAMESPACE_LABELS": `
    {{- /* 1. Get mandatory exemption from helper */ -}}
    {{- $defaults := include "gatekeeper.mandatoryNamespaceExemption" . | fromYaml -}}
    {{- /* 2. Merge user values with mandatory exemption. */ -}}
    {{- $merged := merge (deepCopy .Values.validatingWebhookExemptNamespacesLabels) $defaults -}}
    {{- range $key, $value := $merged }}
    - key: {{ $key }}
      operator: NotIn
      values:
      {{- /* Ensure current namespace is in the list for the metadata key */ -}}
      {{- $list := $value -}}
      {{- if eq $key "kubernetes.io/metadata.name" }}
        {{- $list = append $value $.Release.Namespace | uniq -}}
      {{- end }}
      {{- range $list }}
      - {{ . }}
      {{- end }}
    {{- end }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_OBJECT_SELECTOR": `{{ toYaml .Values.validatingWebhookObjectSelector | nindent 4 }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_CHECK_IGNORE_FAILURE_POLICY": `{{ .Values.validatingWebhookCheckIgnoreFailurePolicy }}`,

	"- HELMSUBST_VALIDATING_WEBHOOK_CHECK_IGNORE_OPERATION_RULES": `- apiGroups:
    - ""
    apiVersions:
    - '*'
    operations:
    - CREATE
    - UPDATE
    resources:
    - namespaces
    scope: '*'`,

	"HELMSUBST_VALIDATING_WEBHOOK_CLIENT_CONFIG: \"\"": `{{- if .Values.validatingWebhookURL }}
    url: https://{{ .Values.validatingWebhookURL }}/v1/admit
    {{- else }}
    service:
      name: gatekeeper-webhook-service
      namespace: '{{ .Release.Namespace }}'
      path: /v1/admit
    {{- end }}`,

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
    {{- if .Values.enableConnectOperations }}
    - CONNECT
    {{- end }}
    resources:
    - '*'
    # Explicitly list all known subresources except "status" (to avoid destabilizing the cluster and increasing load on gatekeeper).
    # You can find a rough list of subresources by doing a case-sensitive search in the Kubernetes codebase for 'Subresource("'
    {{- range .Values.validatingWebhookSubResources }}
    - {{ . }}
    {{- end }}
    scope: '{{ .Values.validatingWebhookScope }}'
  {{- end }}`,

	"HELMSUBST_MUTATING_WEBHOOK_MATCH_CONDITIONS": `{{ toYaml .Values.mutatingWebhookMatchConditions | nindent 4 }}`,

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
    {{- if .Values.service.ipFamilyPolicy }}
  ipFamilyPolicy: {{ .Values.service.ipFamilyPolicy }}
    {{- end }}
    {{- if .Values.service.ipFamilies }}
  ipFamilies: {{ toYaml .Values.service.ipFamilies | nindent 4 }}
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
        {{- end }}
        {{- if and (has "opentelemetry" .Values.metricsBackends) (hasKey .Values "otlpEndpoint") }}
        - --otlp-endpoint={{ .Values.otlpEndpoint }}
        {{- end }}
        {{- if and (has "opentelemetry" .Values.metricsBackends) (hasKey .Values "otlpMetricInterval") }}
        - --otlp-metric-interval={{ .Values.otlpMetricInterval }}
        {{- end }}
        {{- if and (has "stackdriver" .Values.metricsBackends) (hasKey .Values "stackdriverOnlyWhenAvailable") }}
        - --stackdriver-only-when-available={{ .Values.stackdriverOnlyWhenAvailable }}
        {{- end }}
        {{- if and (has "stackdriver" .Values.metricsBackends) (hasKey .Values "stackdriverMetricInterval") }}
        - --stackdriver-metric-interval={{ .Values.stackdriverMetricInterval }}
        {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_EXEMPT_NAMESPACE_PREFIXES": `
        {{- range .Values.controllerManager.exemptNamespacePrefixes}}
        - --exempt-namespace-prefix={{ . }}
        {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_EXEMPT_NAMESPACE_SUFFIXES": `
        {{- range .Values.controllerManager.exemptNamespaceSuffixes}}
        - --exempt-namespace-suffix={{ . }}
        {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_LOGFILE": `
        {{- if .Values.controllerManager.logFile}}
        - --log-file={{ .Values.controllerManager.logFile }}
        {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_AUDIT_LOGFILE": `
        {{- if .Values.audit.logFile}}
        - --log-file={{ .Values.audit.logFile }}
        {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_DEFAULT_CREATE_VAP_FOR_TEMPLATES": `
        {{- if hasKey .Values "defaultCreateVAPForTemplates"}}
        - --default-create-vap-for-templates={{ .Values.defaultCreateVAPForTemplates }}
        {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_DEFAULT_CREATE_VAPB_FOR_CONSTRAINTS": `
        {{- if hasKey .Values "defaultCreateVAPBindingForConstraints"}}
        - --default-create-vap-binding-for-constraints={{ .Values.defaultCreateVAPBindingForConstraints }}
        {{- end }}`,

	"- HELMSUBST_DEPLOYMENT_DEFAULT_WAIT_VAPB_GENERATION": `
        {{- if hasKey .Values "defaultWaitForVAPBGeneration"}}
        - --default-wait-for-vapb-generation={{ .Values.defaultWaitForVAPBGeneration }}
        {{- end }}`,

	"- HELMSUBST_LOG_LEVEL_KEY": `{{ if hasKey .Values "logLevelKey" }}- --log-level-key={{ .Values.logLevelKey }}{{- end }}`,

	"- HELMSUBST_LOG_LEVEL_ENCODER": `{{ if hasKey .Values "logLevelEncoder" }}- --log-level-encoder={{ .Values.logLevelEncoder }}{{- end }}`,

	"- HELMSUBST_METRICS_ADDR": `{{ if hasKey .Values "metricsAddr" }}- --metrics-addr={{ .Values.metricsAddr }}{{- end }}`,

	"- HELMSUBST_READINESS_RETRIES": `{{ if hasKey .Values "readinessRetries" }}- --readiness-retries={{ .Values.readinessRetries }}{{- end }}`,

	"- HELMSUBST_ENABLE_PPROF": `{{ if hasKey .Values "enablePprof" }}- --enable-pprof={{ .Values.enablePprof }}{{- end }}`,

	"- HELMSUBST_PPROF_PORT": `{{ if hasKey .Values "pprofPort" }}- --pprof-port={{ .Values.pprofPort }}{{- end }}`,

	"- HELMSUBST_DISABLE_ENFORCEMENTACTION_VALIDATION": `{{ if hasKey .Values "disableEnforcementActionValidation" }}- --disable-enforcementaction-validation={{ .Values.disableEnforcementActionValidation }}{{- end }}`,

	"- HELMSUBST_ENABLE_REFERENTIAL_RULES": `{{ if hasKey .Values "enableReferentialRules" }}- --enable-referential-rules={{ .Values.enableReferentialRules }}{{- end }}`,

	"- HELMSUBST_CONTROLLER_MANAGER_HOST": `{{ if hasKey .Values.controllerManager "host" }}- --host={{ .Values.controllerManager.host }}{{- end }}`,

	"- HELMSUBST_CONTROLLER_MANAGER_CERT_DIR": `{{ if hasKey .Values.controllerManager "certDir" }}- --cert-dir={{ .Values.controllerManager.certDir }}{{- end }}`,

	"- HELMSUBST_CONTROLLER_MANAGER_CERT_SERVICE_NAME": `{{ if hasKey .Values.controllerManager "certServiceName" }}- --cert-service-name={{ .Values.controllerManager.certServiceName }}{{- end }}`,

	"- HELMSUBST_CONTROLLER_MANAGER_CLIENT_CA_NAME": `{{ if hasKey .Values.controllerManager "clientCAName" }}- --client-ca-name={{ .Values.controllerManager.clientCAName }}{{- end }}`,

	"- HELMSUBST_CONTROLLER_MANAGER_CLIENT_CN_NAME": `{{ if hasKey .Values.controllerManager "clientCNName" }}- --client-cn-name={{ .Values.controllerManager.clientCNName }}{{- end }}`,

	"- HELMSUBST_AUDIT_API_CACHE_DIR": `{{ if hasKey .Values.audit "apiCacheDir" }}- --api-cache-dir={{ .Values.audit.apiCacheDir }}{{- end }}`,

	"- HELMSUBST_SHUTDOWN_DELAY": `{{ if hasKey .Values "shutdownDelay" }}- --shutdown-delay={{ .Values.shutdownDelay }}{{- end }}`,

	"- HELMSUBST_ENABLE_REMOTE_CLUSTER": `{{ if hasKey .Values "enableRemoteCluster" }}- --enable-remote-cluster={{ .Values.enableRemoteCluster }}{{- end }}`,
}
