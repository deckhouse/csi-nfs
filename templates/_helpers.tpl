{{- define "module_klog_verbosity" }}
  {{- $logLevel := .Values.csiNfs.logLevel | default "INFO" }}
  {{- if eq $logLevel "ERROR" -}}
0
  {{- else if eq $logLevel "WARN" -}}
1
  {{- else if eq $logLevel "DEBUG" -}}
4
  {{- else if eq $logLevel "TRACE" -}}
5
  {{- else -}}
2
  {{- end -}}
{{- end }}
