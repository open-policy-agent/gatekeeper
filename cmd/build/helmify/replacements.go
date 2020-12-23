package main

var replacements = map[string]string{
	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_CONTAINER_RESOURCES": `
{{ toYaml .Values.controllerManager.resources | indent 10 }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_CONTAINER_RESOURCES": `
{{ toYaml .Values.audit.resources | indent 10 }}`,

	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_POD_SCHEDULING": `
{{ toYaml .Values.nodeSelector | indent 8 }}
{{- with .Values.affinity }}
      affinity:
{{ toYaml . | indent 8 }}
{{- end }}
      tolerations:
{{ toYaml .Values.tolerations | indent 8 }}
      imagePullSecrets:
{{ toYaml .Values.image.pullSecrets | indent 8 }}
{{- if .Values.controllerManager.priorityClassName }}
      priorityClassName: {{ .Values.controllerManager.priorityClassName }}
{{- end }}`,

	"HELMSUBST_DEPLOYMENT_AUDIT_POD_SCHEDULING": `
{{ toYaml .Values.nodeSelector | indent 8 }}
      affinity:
{{ toYaml .Values.affinity | indent 8 }}
      tolerations:
{{ toYaml .Values.tolerations | indent 8 }}
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
}
