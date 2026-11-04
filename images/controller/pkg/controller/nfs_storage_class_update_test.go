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
	"context"
	"slices"
	"testing"

	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/sds-common-lib/logger"
)

// A StorageClass of an older release gets the new managed-by value with an
// update of the same object, and the annotations and finalizers others put on
// it - the default-class mark Deckhouse sets, a finalizer of another
// controller - stay.
func TestReconcileStorageClassUpdateKeepsForeignMetadata(t *testing.T) {
	const (
		controllerNamespace = "d8-csi-nfs"
		foreignFinalizer    = "example.com/foreign"
	)
	ctx := context.Background()

	nsc := &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: testNSCName},
		Spec: v1alpha1.NFSStorageClassSpec{
			Connection: &v1alpha1.NFSStorageClassConnection{
				Host:       "nfs.example.com",
				Share:      "/export",
				NFSVersion: "4.1",
			},
			ReclaimPolicy:     "Delete",
			VolumeBindingMode: "Immediate",
		},
	}

	existing := ConfigureStorageClass(nil, nsc, controllerNamespace, nil)
	existing.UID = "the-class-of-the-older-release"
	existing.Labels[NFSStorageClassManagedLabelKey] = NFSStorageClassManagedLabelValue
	existing.Annotations[StorageClassDefaultAnnotationKey] = StorageClassDefaultAnnotationValTrue
	existing.Finalizers = []string{foreignFinalizer, NFSStorageClassControllerFinalizerName}

	scheme := apiruntime.NewScheme()
	if err := storagev1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	scList := &storagev1.StorageClassList{}
	if err := cl.List(ctx, scList); err != nil {
		t.Fatalf("listing StorageClasses: %v", err)
	}
	reconcileType, oldSC, newSC := IdentifyReconcileFuncForStorageClass(logger.NewNop(), scList, nsc, controllerNamespace, nil)
	if reconcileType != UpdateReconcile {
		t.Fatalf("reconcile type = %q, want %q", reconcileType, UpdateReconcile)
	}
	if _, err := reconcileStorageClassUpdateFunc(ctx, cl, nil, logger.NewNop(), oldSC, newSC, nsc); err != nil {
		t.Fatalf("reconcileStorageClassUpdateFunc: %v", err)
	}

	got := &storagev1.StorageClass{}
	if err := cl.Get(ctx, client.ObjectKey{Name: testNSCName}, got); err != nil {
		t.Fatalf("getting the StorageClass: %v", err)
	}
	if got.UID != existing.UID {
		t.Errorf("UID = %q, want %q", got.UID, existing.UID)
	}
	if v := got.Labels[NFSStorageClassManagedLabelKey]; v != StorageClassManagedByValue {
		t.Errorf("managed-by = %q, want %q", v, StorageClassManagedByValue)
	}
	if got.Annotations[StorageClassDefaultAnnotationKey] != StorageClassDefaultAnnotationValTrue {
		t.Errorf("annotations = %v, the default-class mark is lost", got.Annotations)
	}
	if !slices.Contains(got.Finalizers, foreignFinalizer) || !slices.Contains(got.Finalizers, NFSStorageClassControllerFinalizerName) {
		t.Errorf("finalizers = %v, want the foreign one and the own one", got.Finalizers)
	}

	// What was written is settled: the next pass finds nothing to do.
	scList = &storagev1.StorageClassList{}
	if err := cl.List(ctx, scList); err != nil {
		t.Fatalf("listing StorageClasses: %v", err)
	}
	if reconcileType, _, _ := IdentifyReconcileFuncForStorageClass(logger.NewNop(), scList, nsc, controllerNamespace, nil); reconcileType != "" {
		t.Errorf("after the update the reconcile type is %q, want none", reconcileType)
	}
}
