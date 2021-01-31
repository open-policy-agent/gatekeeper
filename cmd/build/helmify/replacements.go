package main

var replacements = map[string]string{
	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_CONTAINER_RESOURCES": `
{{ toYaml .Values.controllerManager.resources | indent 10 }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_CONTAINER_RESOURCES": `
{{ toYaml .Values.audit.resources | indent 10 }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_HOST_NETWORK": `{{ .Values.controllerManager.hostNetwork }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_HOST_NETWORK": `{{ .Values.audit.hostNetwork }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_POD_SCHEDULING": `
{{ toYaml .Values.controllerManager.nodeSelector | indent 8 }}
      affinity:
{{ toYaml .Values.controllerManager.affinity | indent 8 }}
      tolerations:
{{ toYaml .Values.controllerManager.tolerations | indent 8 }}
      imagePullSecrets:
{{ toYaml .Values.image.pullSecrets | indent 8 }}
{{- if .Values.controllerManager.priorityClassName }}
      priorityClassName: {{ .Values.controllerManager.priorityClassName }}
{{- end }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_POD_SCHEDULING": `
{{ toYaml .Values.audit.nodeSelector | indent 8 }}
      affinity:
{{ toYaml .Values.audit.affinity | indent 8 }}
      tolerations:
{{ toYaml .Values.audit.tolerations | indent 8 }}
      imagePullSecrets:
{{ toYaml .Values.image.pullSecrets | indent 8 }}
{{- if .Values.audit.priorityClassName }}
      priorityClassName: {{ .Values.audit.priorityClassName }}
{{- end }}`,

	"HELMSUBST_DEPLOYMENT_REPLICAS": `{{ .Values.replicas }}`,

	"HELMSUBST_ANNOTATIONS": `
{{- toYaml .Values.podAnnotations | trim | nindent 8 }}`,

	"HELMSUBST_SECRET_ANNOTATIONS": `
{{- toYaml .Values.secretAnnotations | trim | nindent 4 }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_TIMEOUT": `{{ .Values.validatingWebhookTimeoutSeconds }}`,

	"HELMSUBST_VALIDATING_WEBHOOK_OPERATION_RULES": `
    - CREATE
    - UPDATE
    {{- if .Values.enableDeleteOperations }}
    - DELETE
    {{- end}}`,

    "HELMSUBST_PDB_CONTROLLER_MANAGER_MINAVAILABLE": `{{ .Values.pdb.controllerManager.minAvailable }}`,
}
