{{- /* Written once, then it is yours. */ -}}
{{- /* The chart is named <service>-chart so its ECR repository does not collide
       with the image's. The suffix belongs to the artifact, not the workload,
       so it comes back off here — otherwise every selector label names the
       chart instead of the app. */ -}}
{{- define "chart.name" -}}
{{- .Chart.Name | trimSuffix "-chart" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- /* Without fullnameOverride, resources are named <release>-<chart>, which
       is what you want when several environments share a cluster. Set it to
       drop the release prefix when they do not. */ -}}
{{- define "chart.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "chart.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "chart.selectorLabels" -}}
app.kubernetes.io/name: {{ include "chart.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
