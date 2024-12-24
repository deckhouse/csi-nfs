{{- define "node_driver_registrar_resources" }}
cpu: 12m
memory: 25Mi
{{- end }}

{{- define "node_resources" }}
cpu: 12m
memory: 25Mi
{{- end }}

{{- /* Usage: {{ include "helm_lib_csi_node_manifests" (list . $config) }} */ -}}
{{- define "helm_lib_csi_node_manifests" }}
  {{- $context := index . 0 }}

  {{- $config := index . 1 }}
  {{- $fullname := $config.fullname | default "csi-node" }}
  {{- $nodeImage := $config.nodeImage | required "$config.nodeImage is required" }}
  {{- $driverFQDN := $config.driverFQDN | required "$config.driverFQDN is required" }}
  {{- $serviceAccount := $config.serviceAccount | default "" }}
  {{- $additionalNodeEnvs := $config.additionalNodeEnvs }}
  {{- $additionalNodeArgs := $config.additionalNodeArgs }}
  {{- $additionalNodeVolumes := $config.additionalNodeVolumes }}
  {{- $additionalNodeVolumeMounts := $config.additionalNodeVolumeMounts }}
  {{- $additionalNodeLivenessProbesCmd := $config.additionalNodeLivenessProbesCmd }}
  {{- $additionalNodeSelectorTerms := $config.additionalNodeSelectorTerms }}
  {{- $customNodeSelector := $config.customNodeSelector }}
  {{- $additionalContainers := $config.additionalContainers }}
  {{- $initContainers := $config.initContainers }}

  {{- $kubernetesSemVer := semver $context.Values.global.discovery.kubernetesVersion }}
  {{- $driverRegistrarImageName := join "" (list "csiNodeDriverRegistrar" $kubernetesSemVer.Major $kubernetesSemVer.Minor) }}
  {{- $driverRegistrarImage := include "helm_lib_module_common_image_no_fail" (list $context $driverRegistrarImageName) }}
  {{- if $driverRegistrarImage }}
    {{- if or (include "_helm_lib_cloud_or_hybrid_cluster" $context) ($context.Values.global.enabledModules | has "ceph-csi") ($context.Values.global.enabledModules | has "csi-nfs") ($context.Values.global.enabledModules | has "csi-ceph") ($context.Values.global.enabledModules | has "csi-yadro") ($context.Values.global.enabledModules | has "csi-scsi-generic") ($context.Values.global.enabledModules | has "csi-hpe") ($context.Values.global.enabledModules | has "csi-s3") ($context.Values.global.enabledModules | has "csi-huawei") }}
      {{- if ($context.Values.global.enabledModules | has "vertical-pod-autoscaler-crd") }}
---
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: {{ $fullname }}
  namespace: d8-{{ $context.Chart.Name }}
  {{- include "helm_lib_module_labels" (list $context (dict "app" "csi-node" "workload-resource-policy.deckhouse.io" "every-node")) | nindent 2 }}
spec:
  targetRef:
    apiVersion: "apps/v1"
    kind: DaemonSet
    name: {{ $fullname }}
  updatePolicy:
    updateMode: "Auto"
  resourcePolicy:
    containerPolicies:
    - containerName: "node-driver-registrar"
      minAllowed:
        {{- include "node_driver_registrar_resources" $context | nindent 8 }}
      maxAllowed:
        cpu: 25m
        memory: 50Mi
    - containerName: "node"
      minAllowed:
        {{- include "node_resources" $context | nindent 8 }}
      maxAllowed:
        cpu: 25m
        memory: 50Mi
    {{- end }}
---
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: {{ $fullname }}
  namespace: d8-{{ $context.Chart.Name }}
  {{- include "helm_lib_module_labels" (list $context (dict "app" "csi-node")) | nindent 2 }}

  {{- if eq $context.Chart.Name "csi-nfs" }}
  annotations:
    pod-reloader.deckhouse.io/auto: "true"
  {{- end }}
spec:
  updateStrategy:
    type: RollingUpdate
  selector:
    matchLabels:
      app: {{ $fullname }}
  template:
    metadata:
      labels:
        app: {{ $fullname }}
    spec:
      {{- if $customNodeSelector }}
      {{- $customNodeSelector | toYaml | nindent 6 }}
      {{- else }}
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - operator: In
                key: node.deckhouse.io/type
                values:
                - CloudEphemeral
                - CloudPermanent
                - CloudStatic
                {{- if or (eq $fullname "csi-node-rbd") (eq $fullname "csi-node-cephfs") (eq $fullname "csi-nfs") (eq $fullname "csi-yadro") (eq $fullname "csi-scsi-generic") (eq $fullname "csi-hpe") (eq $fullname "csi-s3") (eq $fullname "csi-huawei") }}
                - Static
                {{- end }}
              {{- if $additionalNodeSelectorTerms }}
              {{- $additionalNodeSelectorTerms | toYaml | nindent 14 }}
              {{- end }}
      {{- end }}
      imagePullSecrets:
      - name: deckhouse-registry
      {{- include "helm_lib_priority_class" (tuple $context "system-node-critical") | nindent 6 }}
      {{- include "helm_lib_tolerations" (tuple $context "any-node" "with-no-csi") | nindent 6 }}
      {{- include "helm_lib_module_pod_security_context_run_as_user_root" . | nindent 6 }}
      {{- if eq $context.Chart.Name "csi-nfs" }}
        {{- print "hostNetwork: false" | nindent 6 }}
      {{- else }}
        {{- print "hostNetwork: true" | nindent 6 }}
      {{- end }}
      dnsPolicy: ClusterFirstWithHostNet
      containers:
      - name: node-driver-registrar
        {{- include "helm_lib_module_container_security_context_not_allow_privilege_escalation" $context | nindent 8 }}
        image: {{ $driverRegistrarImage | quote }}
        args:
        - "--v=5"
        - "--csi-address=$(CSI_ENDPOINT)"
        - "--kubelet-registration-path=$(DRIVER_REG_SOCK_PATH)"
        env:
        - name: CSI_ENDPOINT
          value: "/csi/csi.sock"
        - name: DRIVER_REG_SOCK_PATH
          value: "/var/lib/kubelet/csi-plugins/{{ $driverFQDN }}/csi.sock"
        - name: KUBE_NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
      {{- if $additionalNodeLivenessProbesCmd }}
        livenessProbe:
          initialDelaySeconds: 3
          exec:
            command:
        {{- $additionalNodeLivenessProbesCmd | toYaml | nindent 12 }}
      {{- end }}
        volumeMounts:
        - name: plugin-dir
          mountPath: /csi
        - name: registration-dir
          mountPath: /registration
        resources:
          requests:
            {{- include "helm_lib_module_ephemeral_storage_only_logs" 10 | nindent 12 }}
  {{- if not ($context.Values.global.enabledModules | has "vertical-pod-autoscaler-crd") }}
            {{- include "node_driver_registrar_resources" $context | nindent 12 }}
  {{- end }}
      - name: node
        securityContext:
          privileged: true
        image: {{ $nodeImage }}
        args:
      {{- if $additionalNodeArgs }}
        {{- $additionalNodeArgs | toYaml | nindent 8 }}
      {{- end }}
      {{- if $additionalNodeEnvs }}
        env:
        {{- $additionalNodeEnvs | toYaml | nindent 8 }}
      {{- end }}
        volumeMounts:
        - name: kubelet-dir
          mountPath: /var/lib/kubelet
          mountPropagation: "Bidirectional"
        - name: plugin-dir
          mountPath: /csi
        - name: device-dir
          mountPath: /dev
        {{- if $additionalNodeVolumeMounts }}
          {{- $additionalNodeVolumeMounts | toYaml | nindent 8 }}
        {{- end }}
        resources:
          requests:
            {{- include "helm_lib_module_ephemeral_storage_logs_with_extra" 10 | nindent 12 }}
  {{- if not ($context.Values.global.enabledModules | has "vertical-pod-autoscaler-crd") }}
            {{- include "node_resources" $context | nindent 12 }}
  {{- end }}

      {{- if $additionalContainers }}
        {{- $additionalContainers | toYaml | nindent 6 }}
      {{- end }}

  {{- if $initContainers }}
      initContainers:
    {{- range $initContainer := $initContainers }}
      - resources:
          requests:
            {{- include "helm_lib_module_ephemeral_storage_logs_with_extra" 10 | nindent 12 }}
        {{- $initContainer | toYaml | nindent 8 }}
    {{- end }}
  {{- end }}

      serviceAccount: {{ $serviceAccount | quote }}
      serviceAccountName: {{ $serviceAccount | quote }}
      volumes:
      - name: registration-dir
        hostPath:
          path: /var/lib/kubelet/plugins_registry/
          type: Directory
      - name: kubelet-dir
        hostPath:
          path: /var/lib/kubelet
          type: Directory
      - name: plugin-dir
        hostPath:
          path: /var/lib/kubelet/csi-plugins/{{ $driverFQDN }}/
          type: DirectoryOrCreate
      - name: device-dir
        hostPath:
          path: /dev
          type: Directory

      {{- if $additionalNodeVolumes }}
        {{- $additionalNodeVolumes | toYaml | nindent 6 }}
      {{- end }}

    {{- end }}
  {{- end }}
{{- end }}