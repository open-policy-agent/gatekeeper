
{{/*
Expand the name of the chart.
*/}}
{{- define "gatekeeper.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "gatekeeper.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "gatekeeper.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Adds additional pod labels to the common ones
*/}}
{{- define "gatekeeper.podLabels" -}}
{{- if .Values.podLabels }}
{{- toYaml .Values.podLabels }}
{{- end }}
{{- end -}}

{{/*
Adds additional controller-manager pod labels to the common ones
*/}}
{{- define "controllerManager.podLabels" -}}
{{- if .Values.controllerManager.podLabels }}
{{- toYaml .Values.controllerManager.podLabels }}
{{- end }}
{{- end -}}

{{/*
Adds additional audit pod labels to the common ones
*/}}
{{- define "audit.podLabels" -}}
{{- if .Values.audit.podLabels }}
{{- toYaml .Values.audit.podLabels }}
{{- end }}
{{- end -}}


{{/*
Mandatory labels
*/}}
{{- define "gatekeeper.mandatoryLabels" -}}
app: {{ include "gatekeeper.name" . }}
chart: {{ include "gatekeeper.name" . }}
gatekeeper.sh/system: "yes"
heritage: {{ .Release.Service }}
release: {{ .Release.Name }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "gatekeeper.commonLabels" -}}
helm.sh/chart: {{ include "gatekeeper.chart" . }}
{{ include "gatekeeper.selectorLabels" . }}
{{- if .Chart.Version }}
app.kubernetes.io/version: {{ .Chart.Version | replace "+" "_" | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Values.commonLabels }}
{{ toYaml .Values.commonLabels }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "gatekeeper.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gatekeeper.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Output post install webhook probe container entry
*/}}
{{- define "gatekeeper.postInstallWebhookProbeContainer" -}}
- name: webhook-probe-post
  image: "{{ .Values.postInstall.probeWebhook.image.repository }}:{{ .Values.postInstall.probeWebhook.image.tag }}"
  imagePullPolicy: {{ .Values.postInstall.probeWebhook.image.pullPolicy }}
  command:
    - "curl"
  args:
    - "--retry"
    - "99999"
    - "--retry-connrefused"
    - "--retry-max-time"
    - "{{ .Values.postInstall.probeWebhook.waitTimeout }}"
    - "--retry-delay"
    - "1"
    - "--max-time"
    - "{{ .Values.postInstall.probeWebhook.httpTimeout }}"
    {{- if .Values.postInstall.probeWebhook.insecureHTTPS }}
    - "--insecure"
    {{- else }}
    - "--cacert"
    - /certs/ca.crt
    {{- end }}
    - "-v"
    - "https://gatekeeper-webhook-service.{{ .Release.Namespace }}.svc/v1/admitlabel?timeout=2s"
  resources:
  {{- toYaml .Values.postInstall.resources | nindent 4 }}
  securityContext:
    {{- if .Values.enableRuntimeDefaultSeccompProfile }}
    seccompProfile:
      type: RuntimeDefault
    {{- end }}
  {{- toYaml .Values.postInstall.securityContext | nindent 4 }}
  volumeMounts:
  - mountPath: /certs
    name: cert
    readOnly: true
{{- end -}}

{{/*
Output post install webhook probe volume entry
*/}}
{{- define "gatekeeper.postInstallWebhookProbeVolume" -}}
- name: cert
  secret:
    secretName: {{ .Values.externalCertInjection.secretName }}
{{- end -}}
