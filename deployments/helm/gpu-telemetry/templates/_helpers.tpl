{{- define "gpu-telemetry.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- define "gpu-telemetry.fullname" -}}
{{- if .Values.fullnameOverride }}{{ .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}{{- else }}{{ printf "%s-%s" .Release.Name (include "gpu-telemetry.name" .) | trunc 63 | trimSuffix "-" }}{{- end }}
{{- end }}
{{- define "gpu-telemetry.labels" -}}
app.kubernetes.io/name: {{ include "gpu-telemetry.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end }}
{{- define "gpu-telemetry.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gpu-telemetry.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end }}
{{- define "gpu-telemetry.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}{{ default (include "gpu-telemetry.fullname" .) .Values.serviceAccount.name }}{{- else }}{{ default "default" .Values.serviceAccount.name }}{{- end }}
{{- end }}
