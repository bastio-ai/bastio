{{/*
Expand the name of the chart.
*/}}
{{- define "bastio.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully qualified app name. Truncated to 63 chars (DNS-1123) and
suffix-trimmed so the resulting name doesn't end in a hyphen.
*/}}
{{- define "bastio.fullname" -}}
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
Chart label, e.g. bastio-0.1.0.
*/}}
{{- define "bastio.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels applied to every resource the chart creates.
*/}}
{{- define "bastio.labels" -}}
helm.sh/chart: {{ include "bastio.chart" . }}
{{ include "bastio.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels — only the bare minimum that uniquely picks this
release's pods. Mutating these breaks rolling upgrades, so they
intentionally exclude version + chart labels.
*/}}
{{- define "bastio.selectorLabels" -}}
app.kubernetes.io/name: {{ include "bastio.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
ServiceAccount name — defers to .Values.serviceAccount.name when
set, otherwise derives from the release name.
*/}}
{{- define "bastio.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "bastio.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Secret name — externalSecret.name wins when set, otherwise the
chart-managed secret named after the fullname.
*/}}
{{- define "bastio.secretName" -}}
{{- if .Values.secrets.externalSecret.name -}}
{{- .Values.secrets.externalSecret.name -}}
{{- else -}}
{{- include "bastio.fullname" . -}}
{{- end -}}
{{- end -}}
