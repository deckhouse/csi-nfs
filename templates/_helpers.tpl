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

{{- /*
The client identity tlshd presents on mounts with xprtsec=mtls: the certificate of
tlsParameters.mtls when both its certificate and key are set, the one the hook issues
otherwise. Each helper renders PEM text, empty when there is nothing to render.
*/ -}}
{{- define "csi_nfs_client_cert" -}}
{{- $mtls := .Values.csiNfs.tlsParameters.mtls | default dict -}}
{{- if and $mtls.clientCert $mtls.clientKey -}}
{{- $mtls.clientCert | b64dec -}}
{{- else -}}
{{- .Values.csiNfs.internal.clientTLS.crt | default "" -}}
{{- end -}}
{{- end -}}

{{- define "csi_nfs_client_key" -}}
{{- $mtls := .Values.csiNfs.tlsParameters.mtls | default dict -}}
{{- if and $mtls.clientCert $mtls.clientKey -}}
{{- $mtls.clientKey | b64dec -}}
{{- else -}}
{{- .Values.csiNfs.internal.clientTLS.key | default "" -}}
{{- end -}}
{{- end -}}

{{- define "csi_nfs_client_ca" -}}
{{- $mtls := .Values.csiNfs.tlsParameters.mtls | default dict -}}
{{- if and $mtls.clientCert $mtls.clientKey -}}
{{- if $mtls.clientCA }}{{ $mtls.clientCA | b64dec }}{{ end -}}
{{- else -}}
{{- .Values.csiNfs.internal.clientTLS.ca | default "" -}}
{{- end -}}
{{- end -}}
