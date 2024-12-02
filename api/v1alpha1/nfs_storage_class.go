/*
Copyright 2024 Flant JSC

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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type NFSStorageClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              NFSStorageClassSpec    `json:"spec"`
	Status            *NFSStorageClassStatus `json:"status,omitempty"`
}

type NFSStorageClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []NFSStorageClass `json:"items"`
}

type NFSStorageClassSpec struct {
	Connection        *NFSStorageClassConnection   `json:"connection,omitempty"`
	MountOptions      *NFSStorageClassMountOptions `json:"mountOptions,omitempty"`
	ChmodPermissions  string                       `json:"chmodPermissions,omitempty"`
	ReclaimPolicy     string                       `json:"reclaimPolicy"`
	VolumeBindingMode string                       `json:"volumeBindingMode"`
}

type NFSStorageClassConnection struct {
	Host       string `json:"host"`
	Share      string `json:"share"`
	NFSVersion string `json:"nfsVersion"`
	Tls        bool   `json:"tls"`
	Mtls       bool   `json:"mtls"`
}

type NFSStorageClassMountOptions struct {
	MountMode       string `json:"mountMode,omitempty"`
	Timeout         int    `json:"timeout,omitempty"`
	Retransmissions int    `json:"retransmissions,omitempty"`
	ReadOnly        *bool  `json:"readOnly,omitempty"`
}

type NFSStorageClassStatus struct {
	Phase  string `json:"phase,omitempty"`
	Reason string `json:"reason,omitempty"`
}
