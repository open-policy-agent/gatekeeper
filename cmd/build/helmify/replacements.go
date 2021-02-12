package main

var replacements = map[string]string{
	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_CONTAINER_RESOURCES: ""`: `{{- toYaml .Values.controllerManager.resources | nindent 10 }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_CONTAINER_RESOURCES: ""`: `{{- toYaml .Values.audit.resources | nindent 10 }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_HOST_NETWORK": `{{ .Values.controllerManager.hostNetwork }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_HOST_NETWORK": `{{ .Values.audit.hostNetwork }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_NODE_SELECTOR: ""`: `{{- toYaml .Values.nodeSelector | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_AFFINITY: ""`: `{{- toYaml .Values.affinity | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_TOLERATIONS: ""`: `{{- toYaml .Values.tolerations | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_AUDIT_IMAGE_PULL_SECRETS: ""`: `{{- toYaml .Values.image.pullSecrets | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_NODE_SELECTOR: ""`: `{{- toYaml .Values.nodeSelector | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_AFFINITY: ""`: `{{- toYaml .Values.affinity | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_TOLERATIONS: ""`: `{{- toYaml .Values.tolerations | nindent 8 }}`,

	`HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_IMAGE_PULL_SECRETS: ""`: `{{- toYaml .Values.image.pullSecrets | nindent 8 }}`,

	"HELMSUBST_DEPLOYMENT_REPLICAS": `{{ .Values.replicas }}`,

	`HELMSUBST_ANNOTATIONS: ""`: `{{- toYaml .Values.podAnnotations | trim | nindent 8 }}`,

	"HELMSUBST_SECRET_ANNOTATIONS": `{{- toYaml .Values.secretAnnotations | trim | nindent 4 }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_TIMEOUT": `{{ .Values.validatingWebhookTimeoutSeconds }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_OPERATION_RULES": `
    - CREATE
    - UPDATE
    {{- if .Values.enableDeleteOperations }}
    - DELETE
    {{- end}}`,
}
