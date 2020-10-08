package main

var replacements = map[string]string{
	"HELMSUBST_DEPLOYMENT_CONTROLLER_MANAGER_CONTAINER_RESOURCES": `
{{ toYaml .Values.controllerManager.resources | indent 10 }}`,

"HELMSUBST_DEPLOYMENT_AUDIT_CONTAINER_RESOURCES": `
{{ toYaml .Values.audit.resources | indent 10 }}`,

	"HELMSUBST_DEPLOYMENT_POD_SCHEDULING": `
{{ toYaml .Values.nodeSelector | indent 8 }}
      affinity:
{{ toYaml .Values.affinity | indent 8 }}
      tolerations:
{{ toYaml .Values.tolerations | indent 8 }}`,

	"HELMSUBST_DEPLOYMENT_REPLICAS": `{{ .Values.replicas }}`,

	"HELMSUBST_ANNOTATIONS": `
{{- toYaml .Values.podAnnotations | trim | nindent 8 }}`,
}
