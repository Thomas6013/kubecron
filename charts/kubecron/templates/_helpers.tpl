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

{{/*
Refuse to render a deployment that is reachable from outside the cluster while
authentication is switched off (AUDIT SEC-28).

KubeCron has no authentication of its own: when oidc.enabled is false, the
operator gate in internal/api/server.go becomes a deliberate pass-through and
the auth middleware is never installed at all. Every route is then anonymous —
including POST suspend/resume/trigger, which act on *every* cluster whose
kubeconfig is mounted. A single unauthenticated request can suspend a backup
CronJob fleet-wide.

Both exposure paths are covered, not just the Ingress: a LoadBalancer or
NodePort Service reaches outside the cluster just as effectively.

The escape hatch exists because "ClusterIP behind a trusted mesh/VPN, auth
handled upstream" is a legitimate deployment — but it has to be stated, not
arrived at by leaving a default alone.
*/}}
{{- define "kubecron.validateExposure" -}}
{{- if not .Values.oidc.enabled }}
{{- if not .Values.security.acknowledgeInsecureExposure }}
{{- if .Values.ingress.enabled }}
{{- fail "\n\nSEC-28: ingress.enabled=true with oidc.enabled=false would expose KubeCron with NO authentication.\nEvery endpoint becomes anonymous, including suspend/resume/trigger, which act on every cluster whose kubeconfig is mounted.\n\nChoose one:\n  * set oidc.enabled=true (recommended), or\n  * set security.acknowledgeInsecureExposure=true if the Ingress is on a trusted network and authentication is enforced upstream.\n" }}
{{- end }}
{{- if ne .Values.service.type "ClusterIP" }}
{{- fail (printf "\n\nSEC-28: service.type=%s with oidc.enabled=false would expose KubeCron outside the cluster with NO authentication.\nEvery endpoint becomes anonymous, including suspend/resume/trigger, which act on every cluster whose kubeconfig is mounted.\n\nChoose one:\n  * set oidc.enabled=true (recommended),\n  * keep service.type=ClusterIP and use `kubectl port-forward`, or\n  * set security.acknowledgeInsecureExposure=true if the network is trusted and authentication is enforced upstream.\n" .Values.service.type) }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
