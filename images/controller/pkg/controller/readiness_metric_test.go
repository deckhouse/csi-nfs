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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/sds-common-lib/conditions"
	"github.com/deckhouse/sds-common-lib/monitoring/readiness"
)

func TestReadinessSources(t *testing.T) {
	scheme := apiruntime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&v1alpha1.NFSStorageClass{
			ObjectMeta: metav1.ObjectMeta{Name: "fast"},
			Status: &v1alpha1.NFSStorageClassStatus{Conditions: []metav1.Condition{
				{Type: conditions.TypeReady, Status: metav1.ConditionTrue, Reason: conditions.ReasonReconciled},
			}},
		},
		&v1alpha1.NFSStorageClass{
			ObjectMeta: metav1.ObjectMeta{Name: "broken"},
			Status: &v1alpha1.NFSStorageClassStatus{Conditions: []metav1.Condition{
				{Type: conditions.TypeReady, Status: metav1.ConditionFalse, Reason: conditions.ReasonReconcileFailed},
			}},
		},
	).Build()

	var got []readiness.Resource
	for _, source := range ReadinessSources() {
		resources, err := source(context.Background(), cl)
		if err != nil {
			t.Fatalf("listing: %v", err)
		}
		got = append(got, resources...)
	}

	want := map[string]readiness.Resource{
		"fast":   {Kind: "NFSStorageClass", Name: "fast", Ready: true, Condition: conditions.TypeReady, Reason: conditions.ReasonReconciled},
		"broken": {Kind: "NFSStorageClass", Name: "broken", Condition: conditions.TypeReady, Reason: conditions.ReasonReconcileFailed},
	}
	if len(got) != len(want) {
		t.Fatalf("want %d resources, got %+v", len(want), got)
	}
	for _, r := range got {
		if r != want[r.Name] {
			t.Errorf("%s: want %+v, got %+v", r.Name, want[r.Name], r)
		}
	}
}
