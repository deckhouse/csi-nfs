/*
Copyright 2025 Flant JSC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Interface for managing StorageClass for the `nfs.csi.k8s.io` CSI driver.
//
// Manual creation of StorageClass for this driver is prohibited.
// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=nsc
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels={"heritage=deckhouse","module=csi-nfs","backup.deckhouse.io/cluster-config=true"}
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.reason",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of this resource."
// +crd-enricher:crd:minimal=true
// +crd-enricher:crd:preserveUnknownFields=false
type NFSStorageClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              NFSStorageClassSpec    `json:"spec"`
	Status            *NFSStorageClassStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
type NFSStorageClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []NFSStorageClass `json:"items"`
}

// Defines a Kubernetes StorageClass configuration.
// +k8s:deepcopy-gen=true
// +kubebuilder:validation:XValidation:rule="!(self.connection.mtls == true) || (self.connection.tls == true)",message="If mtls is true, tls must also be true."
// +kubebuilder:validation:XValidation:rule="!has(self.tlsParameters) || self.connection.tls == true",message="tlsParameters require connection.tls to be true."
// +kubebuilder:validation:XValidation:rule="self.reclaimPolicy != 'Retain' || !has(self.volumeCleanup)",message="If reclaimPolicy is 'Retain', volumeCleanup must be omitted."
// +kubebuilder:validation:XValidation:rule="self.connection.nfsVersion == '4.2' || !has(self.volumeCleanup)|| self.volumeCleanup != 'Discard'",message="Discard mode is only available when connection.nfsVersion is '4.2'."
type NFSStorageClassSpec struct {
	// NFS server connection settings.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable."
	Connection *NFSStorageClassConnection `json:"connection,omitempty"`

	// StorageClass mount options.
	// +optional
	MountOptions *NFSStorageClassMountOptions `json:"mountOptions,omitempty"`

	// chmod rights for PVs subdirectory.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-7]{3,4}$`
	ChmodPermissions string `json:"chmodPermissions,omitempty"`

	// Defines the behavior when a PersistentVolumeClaim (PVC) is deleted.
	//
	// Allowed values:
	//
	// - `Delete` — when the PVC is deleted, the associated PersistentVolume (PV) and corresponding data on the NFS server are also deleted;
	// - `Retain` — when the PVC is deleted, the associated PersistentVolume and data on the NFS server are not deleted and require manual removal by the user. See more in [the Kubernetes documentation](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#reclaiming).
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable."
	ReclaimPolicy string `json:"reclaimPolicy"`

	// Defines the volume binding mode.
	//
	// Allowed values:
	//
	// - `Immediate` — the volume is provisioned immediately after the PersistentVolumeClaim (PVC) is created;
	// - `WaitForFirstConsumer` — the volume is provisioned only when the PVC is first used by a pod. See more in [the Kubernetes documentation](https://kubernetes.io/docs/concepts/storage/storage-classes/#volume-binding-mode).
	// +kubebuilder:validation:Enum=Immediate;WaitForFirstConsumer
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable."
	VolumeBindingMode string `json:"volumeBindingMode"`

	// +optional
	// +crd-enricher:raw:minProperties=1
	WorkloadNodes *NFSStorageClassWorkloadNodes `json:"workloadNodes,omitempty"`

	// > This feature is available in Enterprise Edition.
	//
	// Specifies the cleanup method to be applied to the PV's subdirectory content before deletion.
	//
	// If the parameter is omitted, the NFS CSI driver deletes the directory created for the Persistent Volume (PV) on the NFS server without performing any data cleanup.
	// If the parameter is set, the driver erases each file in the PV directory.
	//
	// Allowed values:
	//
	// - `Discard` — uses the filesystem's discard (trim) functionality to free data blocks (this option is available only when supported, for example with NFSv4.2);
	// - `RandomFillSinglePass` — overwrites the content of each file once with random data before deletion. This is implemented by invoking the utility `shred`;
	// - `RandomFillThreePass` — overwrites the content of each file three times with random data before deletion. This is implemented by invoking the utility `shred`.
	// +optional
	// +kubebuilder:validation:Enum=Discard;RandomFillSinglePass;RandomFillThreePass
	// +crd-enricher:raw:x-doc-d8editions=[ee, fe]
	VolumeCleanup string `json:"volumeCleanup,omitempty"`

	// > Available in SE, SE+, EE, and FE editions.
	//
	// RPC-with-TLS parameters of the class, for `connection.tls: true`. With them a class with TLS does not need `tlsParameters.ca` of the module.
	// +optional
	// +crd-enricher:raw:x-doc-d8editions=[se, se-plus, ee, fe]
	TLSParameters *NFSStorageClassTLSParameters `json:"tlsParameters,omitempty"`
}

// +k8s:deepcopy-gen=true
type NFSStorageClassTLSParameters struct {
	// Root certificate in PEM format, encoded in Base64, that verifies the certificate of the NFS server of this class. The nodes trust it together with the CAs of the other classes and `tlsParameters.ca` of the module: the truststore of a node is one for all its mounts, so a class adds a CA rather than limiting which servers its volumes trust.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9+/]*={0,2}$`
	CA string `json:"ca"`
}

// +k8s:deepcopy-gen=true
type NFSStorageClassConnection struct {
	// NFS server host.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable."
	Host string `json:"host"`

	// NFS server share path.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable."
	Share string `json:"share"`

	// NFS version.
	// +kubebuilder:validation:Enum="3";"4.1";"4.2"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable."
	NFSVersion string `json:"nfsVersion"`

	// > Available in SE, SE+, EE, and FE editions.
	//
	// Whether to use TLS for connection.
	// +optional
	// +kubebuilder:default=false
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable."
	// +crd-enricher:raw:x-doc-d8editions=[se, se-plus, ee, fe]
	Tls bool `json:"tls"`

	// > Available in SE, SE+, EE, and FE editions.
	//
	// Whether to use mTLS — requires TLS to be enabled.
	// +optional
	// +kubebuilder:default=false
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="Value is immutable."
	// +crd-enricher:raw:x-doc-d8editions=[se, se-plus, ee, fe]
	Mtls bool `json:"mtls"`
}

// +k8s:deepcopy-gen=true
type NFSStorageClassMountOptions struct {
	// NFS share mount mode.
	// +optional
	// +kubebuilder:validation:Enum=hard;soft
	MountMode string `json:"mountMode,omitempty"`

	// The time during which the NFS client waits for a response before repeating the NFS request, in tenths of a second.
	//
	// For example, 600 is 60 seconds.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Timeout int `json:"timeout,omitempty"`

	// The number of repeated attempts by the NFS client to execute the request before it takes further steps to restore the connection.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Retransmissions int `json:"retransmissions,omitempty"`

	// Share read-only flag.
	// +optional
	ReadOnly *bool `json:"readOnly,omitempty"`
}

// +k8s:deepcopy-gen=true
type NFSStorageClassWorkloadNodes struct {
	// The selector subtree is a vendored metav1.LabelSelector, so everything below
	// it comes from apimachinery unless overridden here. Two things are worth
	// knowing about the result.
	//
	// x-kubernetes-map-type: atomic is apimachinery's own choice -- LabelSelector
	// carries +structType=atomic (pkg/apis/meta/v1/types.go). The hand-written
	// manifest omitted it; the generated one restores it, and that is the wanted
	// behaviour: a selector merged field-by-field between two appliers is a
	// selector neither of them asked for.
	//
	// The description overrides restore wording written in terms of nodes rather
	// than of Go field names. Upstream opens each sentence with the field's own
	// name and adds "This array is replaced during a strategic merge patch", which
	// says nothing about a field of a CRD, and the doc-ru overlay would have had to
	// translate it.
	//
	// Kept detached from the doc comment below by a blank line on purpose: plain
	// text touching the field is its doc comment, and controller-gen renders that
	// into the CRD description.

	// Node selector to specify rules for selecting nodes where Persistent Volumes (PVs) created by this StorageClass are allowed to connect.
	//
	// Combines simple label matches and advanced matching expressions.
	//
	// If this parameter is omitted, NFS shares can be mounted on any node in the cluster running the Linux OS.
	// +optional
	// +crd-enricher:raw:minProperties=1
	// +crd-enricher:raw:properties.matchLabels.description="A map of labels that must match exactly with the labels of a node.\n\nNodes that do not match any of the specified labels will be excluded."
	// +crd-enricher:raw:properties.matchExpressions.description="A list of advanced node selector requirements.\n\nEach requirement specifies a key, an operator, and optional values for filtering nodes based on their labels or other fields."
	// +crd-enricher:raw:properties.matchExpressions.items.properties.operator.enum=[In, NotIn, Exists, DoesNotExist]
	// +crd-enricher:raw:properties.matchExpressions.items.description=A label selector requirement -- a key, an operator and, for some operators, a list of values.
	// +crd-enricher:raw:properties.matchExpressions.items.properties.key.description=The label key that the requirement applies to.
	// +crd-enricher:raw:properties.matchExpressions.items.properties.operator.description=The operator to apply for the key.
	// +crd-enricher:raw:properties.matchExpressions.items.properties.values.description=A list of string values. Required for operators such as `In` or `NotIn`, and ignored for `Exists` or `DoesNotExist`.
	NodeSelector *metav1.LabelSelector `json:"nodeSelector"`
}

// Displays current information about the StorageClass.
// +k8s:deepcopy-gen=true
type NFSStorageClassStatus struct {
	// The StorageClass current state.
	//
	// Possible values:
	//
	// - `Failed` — the controller received incorrect resource configuration or some errors occurred during the operation;
	// - `Created` — the StorageClass was created successfully.
	// +kubebuilder:validation:Enum=Failed;Created
	Phase string `json:"phase,omitempty"`

	// Additional information about the current state of the StorageClass.
	Reason string `json:"reason,omitempty"`

	// The value of `metadata.generation` the controller has last acted on. When it trails
	// `metadata.generation`, the controller has not yet processed the latest spec, and the
	// conditions below still describe the previous one.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// The latest observations of the resource state. Condition types: `Ready`, `RootSquash`.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +crd-enricher:unset:items.description
	// +crd-enricher:raw:items.properties.type.description="Condition type.\n\n`Ready` is `True` once the StorageClass, the mount-options secret and the VolumeSnapshotClass are in place, `False` when a reconcile pass failed, and `Unknown` before the controller has observed the resource.\n\n`RootSquash` is `True` when the NFS server maps root to another user on the export (`root_squash` or `all_squash`), `False` when root keeps its identity, and `Unknown` when the check could not reach a verdict: the export could not be mounted (reason `MountFailed`), it is read-only (`ExportReadOnly`), or the check failed otherwise (`ProbeFailed`). The CSI controller checks the export when the NFSStorageClass is created and every 10 minutes after that. The condition only warns and does not affect `Ready`."
	// +crd-enricher:raw:items.properties.status.description=Current status of the condition.
	// +crd-enricher:raw:items.properties.observedGeneration.description=The value of `metadata.generation` this condition was set against.
	// +crd-enricher:raw:items.properties.lastTransitionTime.description="Timestamp of the last status transition for this condition. It is not bumped\nwhen only the reason or the message changes."
	// +crd-enricher:raw:items.properties.reason.description="Machine-readable reason for the current status, suitable for log filtering\nand alerting."
	// +crd-enricher:raw:items.properties.message.description=Human-readable explanation of the current status.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
