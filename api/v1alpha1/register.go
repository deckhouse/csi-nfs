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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	NFSStorageClassKind = "NFSStorageClass"
	APIGroup            = "storage.deckhouse.io"
	APIVersion          = "v1alpha1"
	APIGroupMC          = "deckhouse.io"
)

// SchemeGroupVersion is group version used to register these objects
var (
	SchemeGroupVersion = schema.GroupVersion{
		Group:   APIGroup,
		Version: APIVersion,
	}

	SchemeGroupVersionMC = schema.GroupVersion{
		Group:   APIGroupMC,
		Version: APIVersion,
	}
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme   = SchemeBuilder.AddToScheme
)

// Adds the list of known types to Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&NFSStorageClass{},
		&NFSStorageClassList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersionMC)
	return nil
}
