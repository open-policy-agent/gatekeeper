
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
{{- toYaml .Values.podLabels | nindent 8 }}
{{- end }}
{{- end -}}

{{/*
Output post install webhook probe container entry
*/}}
{{- define "gatekeeper.postInstallWebhookProbeContainer" -}}
- name: webhook-probe-post
  image: "{{ .Values.postInstall.probeWebhook.image.repository }}:{{ .Values.postInstall.probeWebhook.image.tag }}"
  imagePullPolicy: {{ .Values.postInstall.probeWebhook.image.pullPolicy }}
  args:
    - "curl"
    - "--retry"
    - "99999"
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
    secretName: gatekeeper-webhook-server-cert
{{- end -}}
