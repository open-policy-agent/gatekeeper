package main

// #!/bin/bash
// scriptdir="$(dirname "$0")"
// cd "$scriptdir"
// cp ./../../deploy/gatekeeper.yaml ${PWD}/helm-modifications/_temp.yaml
// kustomize build helm-modifications -o templates/gatekeeper.yaml

var replacements = map[string]string{
	"HELMSUBST_DEPLOYMENT_CONTAINER_RESOURCES": `
{{ toYaml .Values.resources | indent 10 }}`,

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
