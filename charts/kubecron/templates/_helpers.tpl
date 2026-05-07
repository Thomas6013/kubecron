{{/*
Expand the name of the chart.
*/}}
{{- define "kubecron.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "kubecron.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart label.
*/}}
{{- define "kubecron.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "kubecron.labels" -}}
helm.sh/chart: {{ include "kubecron.chart" . }}
{{ include "kubecron.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "kubecron.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubecron.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "kubecron.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "kubecron.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Image reference.
*/}}
{{- define "kubecron.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
Name of the kubeconfigs Secret to mount.
*/}}
{{- define "kubecron.kubeconfigsSecretName" -}}
{{- if .Values.kubeconfigs.existingSecret }}
{{- .Values.kubeconfigs.existingSecret }}
{{- else }}
{{- printf "%s-kubeconfigs" (include "kubecron.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Name of the OIDC Secret.
*/}}
{{- define "kubecron.oidcSecretName" -}}
{{- if .Values.oidc.existingSecret }}
{{- .Values.oidc.existingSecret }}
{{- else }}
{{- printf "%s-oidc" (include "kubecron.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Name of the PVC.
*/}}
{{- define "kubecron.pvcName" -}}
{{- if .Values.persistence.existingClaim }}
{{- .Values.persistence.existingClaim }}
{{- else }}
{{- printf "%s-data" (include "kubecron.fullname" .) }}
{{- end }}
{{- end }}
