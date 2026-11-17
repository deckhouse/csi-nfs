/*
Copyright 2026 Flant JSC

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

package crdtests

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/deckhouse/csi-nfs/api/v1alpha1"
)

const (
	mtlsNeedsTLS           = "If mtls is true, tls must also be true."
	tlsParametersNeedTLS   = "tlsParameters require connection.tls to be true."
	retainWithoutCleanup   = "If reclaimPolicy is 'Retain', volumeCleanup must be omitted."
	discardNeedsNFS42      = "Discard mode is only available when connection.nfsVersion is '4.2'."
	immutable              = "Value is immutable."
	connectionImmutable    = "spec.connection: Invalid value: \"object\": " + immutable
	unsupportedValue       = "Unsupported value"
	placeholderCA          = "placeholderCA"
	rotatedPlaceholderCA   = "rotatedPlaceholderCA"
	notBase64PlaceholderCA = "placeholder-ca"
	subDirCharacters       = "subDir may contain only Latin letters, digits"
	subDirDotSegments      = "subDir must not contain the path segments '.' and '..'."
	subDirPerPVC           = "subDir must give every PVC a directory of its own"
	subDirNeedsRetain      = "If connection.subDir does not contain ${pv.metadata.name}, reclaimPolicy must be 'Retain'."
	subDirByPV             = "${pvc.metadata.namespace}/${pv.metadata.name}"
	subDirByPVC            = "${pvc.metadata.namespace}_${pvc.metadata.name}"
)

// fieldImmutable is the message of the self == oldSelf rule on a scalar field,
// told apart from the rule on the whole connection by its path.
func fieldImmutable(path string) string {
	return path + ": Invalid value: \"string\": " + immutable
}

func flagImmutable(path string) string {
	return path + ": Invalid value: \"boolean\": " + immutable
}

func nfsClass(name string) *v1alpha1.NFSStorageClass {
	return &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.NFSStorageClassSpec{
			Connection: &v1alpha1.NFSStorageClassConnection{
				Host:       "nfs.example.com",
				Share:      "/export",
				NFSVersion: "4.2",
			},
			ReclaimPolicy:     "Delete",
			VolumeBindingMode: "WaitForFirstConsumer",
		},
	}
}

func tlsClass(name string) *v1alpha1.NFSStorageClass {
	nsc := nfsClass(name)
	nsc.Spec.Connection.Tls = true
	return nsc
}

func nfs41Class(name string) *v1alpha1.NFSStorageClass {
	nsc := nfsClass(name)
	nsc.Spec.Connection.NFSVersion = "4.1"
	return nsc
}

func retainClass(name string) *v1alpha1.NFSStorageClass {
	nsc := nfsClass(name)
	nsc.Spec.ReclaimPolicy = "Retain"
	return nsc
}

func subDirClass(subDir string, nsc func(string) *v1alpha1.NFSStorageClass) func(string) *v1alpha1.NFSStorageClass {
	return func(name string) *v1alpha1.NFSStorageClass {
		c := nsc(name)
		c.Spec.Connection.SubDir = subDir
		return c
	}
}

func TestNFSStorageClassCreate(t *testing.T) {
	for _, tt := range []struct {
		name    string
		nsc     func(name string) *v1alpha1.NFSStorageClass
		wantErr []string
	}{
		{name: "plain", nsc: nfsClass},
		{
			name: "TLS with mTLS and a CA",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := tlsClass(name)
				nsc.Spec.Connection.Mtls = true
				nsc.Spec.TLSParameters = &v1alpha1.NFSStorageClassTLSParameters{CA: placeholderCA}
				return nsc
			},
		},
		{
			name: "mTLS without TLS",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.Connection.Mtls = true
				return nsc
			},
			wantErr: []string{mtlsNeedsTLS},
		},
		{
			name: "tlsParameters without TLS",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.TLSParameters = &v1alpha1.NFSStorageClassTLSParameters{CA: placeholderCA}
				return nsc
			},
			wantErr: []string{tlsParametersNeedTLS},
		},
		{
			name: "empty CA",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := tlsClass(name)
				nsc.Spec.TLSParameters = &v1alpha1.NFSStorageClassTLSParameters{}
				return nsc
			},
			wantErr: []string{"spec.tlsParameters.ca"},
		},
		{
			name: "CA that is not Base64",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := tlsClass(name)
				nsc.Spec.TLSParameters = &v1alpha1.NFSStorageClassTLSParameters{CA: notBase64PlaceholderCA}
				return nsc
			},
			wantErr: []string{"spec.tlsParameters.ca", "should match"},
		},
		{name: "Retain without volumeCleanup", nsc: retainClass},
		{
			name: "Retain with volumeCleanup",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := retainClass(name)
				nsc.Spec.VolumeCleanup = "RandomFillSinglePass"
				return nsc
			},
			wantErr: []string{retainWithoutCleanup},
		},
		{
			name: "Discard on NFS 4.2",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.VolumeCleanup = "Discard"
				return nsc
			},
		},
		{
			name: "Discard on NFS 4.1",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfs41Class(name)
				nsc.Spec.VolumeCleanup = "Discard"
				return nsc
			},
			wantErr: []string{discardNeedsNFS42},
		},
		{
			name: "Discard on NFS 3",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.Connection.NFSVersion = "3"
				nsc.Spec.VolumeCleanup = "Discard"
				return nsc
			},
			wantErr: []string{discardNeedsNFS42},
		},
		{
			name: "random fill on NFS 4.1",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfs41Class(name)
				nsc.Spec.VolumeCleanup = "RandomFillThreePass"
				return nsc
			},
		},
		{
			name: "without connection",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.Connection = nil
				return nsc
			},
			wantErr: []string{"spec.connection: Required value"},
		},
		{
			name: "empty host",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.Connection.Host = ""
				return nsc
			},
			wantErr: []string{"spec.connection.host"},
		},
		{
			name: "empty share",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.Connection.Share = ""
				return nsc
			},
			wantErr: []string{"spec.connection.share"},
		},
		{
			name: "unknown NFS version",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.Connection.NFSVersion = "4.0"
				return nsc
			},
			wantErr: []string{"spec.connection.nfsVersion", unsupportedValue},
		},
		{
			name: "unknown reclaim policy",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.ReclaimPolicy = "Recycle"
				return nsc
			},
			wantErr: []string{"spec.reclaimPolicy", unsupportedValue},
		},
		{
			name: "unknown volume binding mode",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.VolumeBindingMode = "Later"
				return nsc
			},
			wantErr: []string{"spec.volumeBindingMode", unsupportedValue},
		},
		{
			name: "unknown volume cleanup",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.VolumeCleanup = "Zero"
				return nsc
			},
			wantErr: []string{"spec.volumeCleanup", unsupportedValue},
		},
		{
			name: "unknown mount mode",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.MountOptions = &v1alpha1.NFSStorageClassMountOptions{MountMode: "medium"}
				return nsc
			},
			wantErr: []string{"spec.mountOptions.mountMode", unsupportedValue},
		},
		{
			name: "negative timeout and retransmissions",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.MountOptions = &v1alpha1.NFSStorageClassMountOptions{Timeout: -1, Retransmissions: -1}
				return nsc
			},
			wantErr: []string{"spec.mountOptions.timeout", "spec.mountOptions.retransmissions"},
		},
		{
			name: "four-digit chmod",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.ChmodPermissions = "2775"
				return nsc
			},
		},
		{
			name: "chmod with a non-octal digit",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.ChmodPermissions = "778"
				return nsc
			},
			wantErr: []string{"spec.chmodPermissions", "should match"},
		},
		{
			name: "empty node selector",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := nfsClass(name)
				nsc.Spec.WorkloadNodes = &v1alpha1.NFSStorageClassWorkloadNodes{NodeSelector: &metav1.LabelSelector{}}
				return nsc
			},
			wantErr: []string{"spec.workloadNodes.nodeSelector"},
		},
		{name: "subDir with the PV name and Delete", nsc: subDirClass(subDirByPV, nfsClass)},
		{name: "subDir with the PV name alone", nsc: subDirClass("${pv.metadata.name}", nfsClass)},
		{name: "subDir by namespace and PVC name with Retain", nsc: subDirClass(subDirByPVC, retainClass)},
		{name: "subDir with a prefix and the name before the namespace", nsc: subDirClass("site-a/${pvc.metadata.name}.${pvc.metadata.namespace}", retainClass)},
		{name: "subDir with dots inside a segment", nsc: subDirClass("a..b/.x/${pv.metadata.name}", nfsClass)},
		{
			name:    "subDir without the PV name and Delete",
			nsc:     subDirClass(subDirByPVC, nfsClass),
			wantErr: []string{subDirNeedsRetain},
		},
		{
			name:    "subDir by the PVC name alone",
			nsc:     subDirClass("${pvc.metadata.name}", retainClass),
			wantErr: []string{subDirPerPVC},
		},
		{
			name:    "subDir by the namespace alone",
			nsc:     subDirClass("${pvc.metadata.namespace}", retainClass),
			wantErr: []string{subDirPerPVC},
		},
		{
			name:    "constant subDir",
			nsc:     subDirClass("shared", retainClass),
			wantErr: []string{subDirPerPVC},
		},
		{
			name:    "subDir joining namespace and PVC name with a dash",
			nsc:     subDirClass("${pvc.metadata.namespace}-${pvc.metadata.name}", retainClass),
			wantErr: []string{subDirPerPVC},
		},
		{
			name:    "subDir joining namespace and PVC name with nothing",
			nsc:     subDirClass("${pvc.metadata.namespace}${pvc.metadata.name}", retainClass),
			wantErr: []string{subDirPerPVC},
		},
		{
			name:    "subDir with an unknown placeholder",
			nsc:     subDirClass("${pvc.metadata.labels.app}/${pv.metadata.name}", nfsClass),
			wantErr: []string{subDirCharacters},
		},
		{
			name:    "subDir with the volume ID separator",
			nsc:     subDirClass("a#b/${pv.metadata.name}", nfsClass),
			wantErr: []string{subDirCharacters},
		},
		{
			name:    "subDir with a leading slash",
			nsc:     subDirClass("/${pv.metadata.name}", nfsClass),
			wantErr: []string{subDirCharacters},
		},
		{
			name:    "subDir with a trailing slash",
			nsc:     subDirClass("${pv.metadata.name}/", nfsClass),
			wantErr: []string{subDirCharacters},
		},
		{
			name:    "subDir with an empty segment",
			nsc:     subDirClass("a//${pv.metadata.name}", nfsClass),
			wantErr: []string{subDirCharacters},
		},
		{
			name:    "subDir leaving the share",
			nsc:     subDirClass("../${pv.metadata.name}", nfsClass),
			wantErr: []string{subDirDotSegments},
		},
		{
			name:    "subDir with a dot segment",
			nsc:     subDirClass("a/./${pv.metadata.name}", nfsClass),
			wantErr: []string{subDirDotSegments},
		},
		{
			name:    "subDir ending in a parent segment",
			nsc:     subDirClass("${pv.metadata.name}/..", nfsClass),
			wantErr: []string{subDirDotSegments},
		},
		{
			name:    "subDir longer than 255 characters",
			nsc:     subDirClass(strings.Repeat("a", 240)+"/${pv.metadata.name}", nfsClass),
			wantErr: []string{"spec.connection.subDir", "Too long"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			nsc := tt.nsc(objectName("create", tt.name))
			err := k8sClient.Create(context.Background(), nsc)
			if err == nil {
				t.Cleanup(func() { _ = k8sClient.Delete(context.Background(), nsc) })
			}
			expectAdmission(t, err, tt.wantErr...)
		})
	}
}

func TestNFSStorageClassUpdate(t *testing.T) {
	for _, tt := range []struct {
		name    string
		nsc     func(name string) *v1alpha1.NFSStorageClass
		change  func(*v1alpha1.NFSStorageClass)
		wantErr []string
	}{
		{
			name:    "host",
			nsc:     nfsClass,
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.Connection.Host = "other.example.com" },
			wantErr: []string{fieldImmutable("spec.connection.host"), connectionImmutable},
		},
		{
			name:    "share",
			nsc:     nfsClass,
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.Connection.Share = "/other" },
			wantErr: []string{fieldImmutable("spec.connection.share"), connectionImmutable},
		},
		{
			name:    "nfsVersion",
			nsc:     nfsClass,
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.Connection.NFSVersion = "4.1" },
			wantErr: []string{fieldImmutable("spec.connection.nfsVersion"), connectionImmutable},
		},
		{
			name:    "tls turned on",
			nsc:     nfsClass,
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.Connection.Tls = true },
			wantErr: []string{flagImmutable("spec.connection.tls"), connectionImmutable},
		},
		{
			name:    "tls turned off",
			nsc:     tlsClass,
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.Connection.Tls = false },
			wantErr: []string{flagImmutable("spec.connection.tls"), connectionImmutable},
		},
		{
			name:    "mtls turned on",
			nsc:     tlsClass,
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.Connection.Mtls = true },
			wantErr: []string{flagImmutable("spec.connection.mtls"), connectionImmutable},
		},
		{
			name:    "subDir",
			nsc:     subDirClass(subDirByPV, nfsClass),
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.Connection.SubDir = "other/${pv.metadata.name}" },
			wantErr: []string{fieldImmutable("spec.connection.subDir"), connectionImmutable},
		},
		{
			name:    "subDir added",
			nsc:     nfsClass,
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.Connection.SubDir = subDirByPV },
			wantErr: []string{connectionImmutable},
		},
		{
			name:    "subDir removed",
			nsc:     subDirClass(subDirByPV, nfsClass),
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.Connection.SubDir = "" },
			wantErr: []string{connectionImmutable},
		},
		{
			name:    "reclaimPolicy",
			nsc:     nfsClass,
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.ReclaimPolicy = "Retain" },
			wantErr: []string{fieldImmutable("spec.reclaimPolicy")},
		},
		{
			name:    "volumeBindingMode",
			nsc:     nfsClass,
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.VolumeBindingMode = "Immediate" },
			wantErr: []string{fieldImmutable("spec.volumeBindingMode")},
		},
		{
			name:    "volumeCleanup added to a Retain class",
			nsc:     retainClass,
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.VolumeCleanup = "RandomFillSinglePass" },
			wantErr: []string{retainWithoutCleanup},
		},
		{
			name:    "Discard added on NFS 4.1",
			nsc:     nfs41Class,
			change:  func(c *v1alpha1.NFSStorageClass) { c.Spec.VolumeCleanup = "Discard" },
			wantErr: []string{discardNeedsNFS42},
		},
		{
			name: "tlsParameters added without TLS",
			nsc:  nfsClass,
			change: func(c *v1alpha1.NFSStorageClass) {
				c.Spec.TLSParameters = &v1alpha1.NFSStorageClassTLSParameters{CA: placeholderCA}
			},
			wantErr: []string{tlsParametersNeedTLS},
		},
		{
			name: "CA is mutable",
			nsc: func(name string) *v1alpha1.NFSStorageClass {
				nsc := tlsClass(name)
				nsc.Spec.TLSParameters = &v1alpha1.NFSStorageClassTLSParameters{CA: placeholderCA}
				return nsc
			},
			change: func(c *v1alpha1.NFSStorageClass) { c.Spec.TLSParameters.CA = rotatedPlaceholderCA },
		},
		{
			name: "mount options, chmod, nodes and cleanup are mutable",
			nsc:  nfsClass,
			change: func(c *v1alpha1.NFSStorageClass) {
				readOnly := true
				c.Spec.MountOptions = &v1alpha1.NFSStorageClassMountOptions{
					MountMode:       "soft",
					Timeout:         600,
					Retransmissions: 3,
					ReadOnly:        &readOnly,
				}
				c.Spec.ChmodPermissions = "0770"
				c.Spec.WorkloadNodes = &v1alpha1.NFSStorageClassWorkloadNodes{
					NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"storage": "nfs"}},
				}
				c.Spec.VolumeCleanup = "Discard"
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			nsc := tt.nsc(objectName("update", tt.name))
			if err := k8sClient.Create(context.Background(), nsc); err != nil {
				t.Fatalf("create NFSStorageClass %s: %v", nsc.Name, err)
			}
			t.Cleanup(func() { _ = k8sClient.Delete(context.Background(), nsc) })
			tt.change(nsc)
			expectAdmission(t, k8sClient.Update(context.Background(), nsc), tt.wantErr...)
		})
	}
}
