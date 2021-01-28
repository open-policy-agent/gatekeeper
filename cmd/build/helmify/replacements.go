package main

var replacements = map[string]string{
	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_CONTAINER_RESOURCES: ""`: `{{- toYaml .Values.controllerManager.resources | nindent 10 }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_CONTAINER_RESOURCES: ""`: `{{- toYaml .Values.audit.resources | nindent 10 }}`,

	`HELMSUBST_DEPLOYMENT_NODE_SELECTOR: ""`: `{{- toYaml .Values.nodeSelector | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_AFFINITY: ""`: `{{- toYaml .Values.affinity | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_TOLERATIONS: ""`: `{{- toYaml .Values.tolerations | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_IMAGE_PULL_SECRETS: ""`: `{{- toYaml .Values.image.pullSecrets | nindent 8 }}`,

	"HELMSUBST_DEPLOYMENT_REPLICAS": `{{ .Values.replicas }}`,

	`HELMSUBST_ANNOTATIONS: ""`: `{{- toYaml .Values.podAnnotations | trim | nindent 8 }}`,

	"HELMSUBST_SECRET_ANNOTATIONS": `{{- toYaml .Values.secretAnnotations | trim | nindent 4 }}`,

	`HELMSUBST_VALIDATING_WEBHOOK_TIMEOUT: ""`: `{{- toYaml .Values.validatingWebhookTimeoutSeconds | nindent 8 }}`,


	"HELMSUBST_VALIDATING_WEBHOOK_OPERATION_RULES": `
    - CREATE
    - UPDATE
    {{- if .Values.enableDeleteOperations }}
    - DELETE
    {{- end}}`,
}