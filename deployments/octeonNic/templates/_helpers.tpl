{{/*
Full image name with tag
*/}}
{{- define "octnic.fullimage" -}}
{{- .Values.octnic.repository -}}/{{- .Values.octnic.image -}}:{{- .Values.octnic.version | default .Chart.AppVersion -}}
{{- end }}
