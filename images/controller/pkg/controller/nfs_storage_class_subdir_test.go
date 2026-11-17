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

package controller

import (
	"maps"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
)

const subDirTestNamespace = "d8-csi-nfs"

func subDirClass(subDir string) *v1alpha1.NFSStorageClass {
	return &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: "subdir"},
		Spec: v1alpha1.NFSStorageClassSpec{
			Connection: &v1alpha1.NFSStorageClassConnection{
				Host: "nfs.example.com", Share: "/export", NFSVersion: "4.2", SubDir: subDir,
			},
			ReclaimPolicy:     "Retain",
			VolumeBindingMode: "WaitForFirstConsumer",
		},
	}
}

func TestConfigureStorageClassPassesSubDirVerbatim(t *testing.T) {
	const template = "${pvc.metadata.namespace}_${pvc.metadata.name}"
	sc := ConfigureStorageClass(nil, subDirClass(template), subDirTestNamespace, nil)
	if got, ok := sc.Parameters[SubDirParamKey]; !ok || got != template {
		t.Fatalf("parameter %q = %q (present %v), want %q", SubDirParamKey, got, ok, template)
	}
}

func TestConfigureStorageClassWithoutSubDirKeepsTheParameters(t *testing.T) {
	sc := ConfigureStorageClass(nil, subDirClass(""), subDirTestNamespace, nil)
	want := map[string]string{
		serverParamKey:                "nfs.example.com",
		shareParamKey:                 "/export",
		ProvisionerSecretNameKey:      SecretForMountOptionsPrefix + "subdir",
		ProvisionerSecretNamespaceKey: subDirTestNamespace,
	}
	if !maps.Equal(sc.Parameters, want) {
		t.Fatalf("parameters %v, want %v", sc.Parameters, want)
	}

	needRecreate, diff := CompareStorageClasses(sc, ConfigureStorageClass(sc, subDirClass(""), subDirTestNamespace, nil))
	if needRecreate || diff != "" {
		t.Fatalf("a class without subDir is recreated (%v) or changed: %s", needRecreate, diff)
	}
}
