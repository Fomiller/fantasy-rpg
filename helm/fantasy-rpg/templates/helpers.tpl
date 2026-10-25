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

{{- /* Names of their own, not extra labels, so a Service for one kind of
       pod never picks up another. Selectors cannot change once deployed. */ -}}
{{- define "chart.worldSelectorLabels" -}}
app.kubernetes.io/name: {{ include "chart.name" . }}-world
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "chart.instanceSelectorLabels" -}}
app.kubernetes.io/name: {{ include "chart.name" . }}-instance
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "chart.instanceName" -}}
{{- printf "%s-instance" (include "chart.fullname" .) | trunc 52 | trimSuffix "-" -}}
{{- end -}}

{{- /* The world server's INSTANCES, from the same replica count as the
       StatefulSet, so the two never disagree. */ -}}
{{- define "chart.instancesEnv" -}}
{{- $name := include "chart.instanceName" . -}}
{{- $public := ternary (printf "wss://%s" .Values.ingressRoute.host) "ws://localhost:8080" (ne .Values.ingressRoute.host "") -}}
{{- $list := list -}}
{{- range $i := until (int .Values.instances.replicas) -}}
{{- $pod := printf "%s-%d" $name $i -}}
{{- $list = append $list (printf "%s=http://%s:8080=%s/instances/%s/ws" $pod $pod $public $pod) -}}
{{- end -}}
- name: INSTANCES
  value: {{ join " " $list | quote }}
{{- end -}}

{{- define "chart.joinKeyEnv" -}}
- name: JOIN_TOKEN_KEY
  valueFrom:
    secretKeyRef:
      name: {{ .Values.joinKey.secretName }}
      key: JOIN_TOKEN_KEY
{{- end -}}

{{- define "chart.dbName" -}}
{{- printf "%s-db" (include "chart.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "chart.databaseEnv" -}}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "chart.dbName" . }}-app
      key: uri
{{- end -}}
