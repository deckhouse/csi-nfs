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

package controller_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/controller/pkg/controller"
)

var systemIgnoredPrefixes = []string{
	"app.kubernetes.io/managed-by",
	"app.kubernetes.io/instance",
	"kubernetes.io/",
	"k8s.io/",
	"storage.deckhouse.io/managed-by",
}

var userIgnoredPrefixes = []string{
	"argocd.argoproj.io/",
	"kustomize.toolkit.fluxcd.io/",
	"helm.toolkit.fluxcd.io/",
	"fleet.cattle.op/",
}

func ignoredPrefixesUnion() []string {
	return append(append([]string{}, systemIgnoredPrefixes...), userIgnoredPrefixes...)
}

var _ = Describe("NFSStorageClass label propagation filtering", func() {
	const controllerNamespace = "test-namespace"

	newNsc := func(name string, labels map[string]string) *v1alpha1.NFSStorageClass {
		return &v1alpha1.NFSStorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: labels,
			},
			Spec: v1alpha1.NFSStorageClassSpec{
				Connection: &v1alpha1.NFSStorageClassConnection{
					Host:  "nfs.example.com",
					Share: "/export",
				},
				ReclaimPolicy:     "Delete",
				VolumeBindingMode: "Immediate",
			},
		}
	}

	It("propagates allowed labels and enforces managed-by label", func() {
		nsc := newNsc("nfs-sc-allowed", map[string]string{
			"team":      "storage",
			"workload":  "prod",
			"some/team": "infra",
		})

		sc := controller.ConfigureStorageClass(nil, nsc, controllerNamespace, ignoredPrefixesUnion())

		Expect(sc.Labels).To(HaveKeyWithValue("team", "storage"))
		Expect(sc.Labels).To(HaveKeyWithValue("workload", "prod"))
		Expect(sc.Labels).To(HaveKeyWithValue("some/team", "infra"))
		Expect(sc.Labels).To(HaveKeyWithValue(controller.NFSStorageClassManagedLabelKey, controller.NFSStorageClassManagedLabelValue))
	})

	It("filters labels matching system and user prefixes", func() {
		nsc := newNsc("nfs-sc-filtered", map[string]string{
			"app.kubernetes.io/managed-by":  "argo-cd",
			"app.kubernetes.io/instance":    "csi-nfs-instance",
			"kubernetes.io/region":          "eu-west-1",
			"k8s.io/cluster-name":           "prod",
			"argocd.argoproj.io/secret":     "true",
			"kustomize.toolkit.fluxcd.io/x": "1",
			"helm.toolkit.fluxcd.io/y":      "2",
			"fleet.cattle.op/bundle":        "store",
			"team":                          "storage",
		})

		sc := controller.ConfigureStorageClass(nil, nsc, controllerNamespace, ignoredPrefixesUnion())

		Expect(sc.Labels).To(HaveKeyWithValue("team", "storage"))
		Expect(sc.Labels).To(HaveKeyWithValue(controller.NFSStorageClassManagedLabelKey, controller.NFSStorageClassManagedLabelValue))

		for _, key := range []string{
			"app.kubernetes.io/managed-by",
			"app.kubernetes.io/instance",
			"kubernetes.io/region",
			"k8s.io/cluster-name",
			"argocd.argoproj.io/secret",
			"kustomize.toolkit.fluxcd.io/x",
			"helm.toolkit.fluxcd.io/y",
			"fleet.cattle.op/bundle",
		} {
			Expect(sc.Labels).NotTo(HaveKey(key))
		}
	})

	It("overrides storage.deckhouse.io/managed-by even when NFSSC tries to set it", func() {
		nsc := newNsc("nfs-sc-managed-by-override", map[string]string{
			"storage.deckhouse.io/managed-by": "bogus-controller",
			"team":                            "storage",
		})

		sc := controller.ConfigureStorageClass(nil, nsc, controllerNamespace, ignoredPrefixesUnion())

		Expect(sc.Labels).To(HaveKeyWithValue(controller.NFSStorageClassManagedLabelKey, controller.NFSStorageClassManagedLabelValue))
		Expect(sc.Labels).To(HaveKeyWithValue("team", "storage"))
	})

	It("handles an NFSSC where every label is ignored", func() {
		nsc := newNsc("nfs-sc-all-ignored", map[string]string{
			"argocd.argoproj.io/x":         "1",
			"app.kubernetes.io/managed-by": "argo-cd",
		})

		sc := controller.ConfigureStorageClass(nil, nsc, controllerNamespace, ignoredPrefixesUnion())

		Expect(sc.Labels).To(HaveLen(1))
		Expect(sc.Labels).To(HaveKeyWithValue(controller.NFSStorageClassManagedLabelKey, controller.NFSStorageClassManagedLabelValue))
	})

	It("treats an empty prefix in the ignored list as a no-op", func() {
		nsc := newNsc("nfs-sc-empty-prefix", map[string]string{
			"team": "storage",
		})

		sc := controller.ConfigureStorageClass(nil, nsc, controllerNamespace, []string{""})

		Expect(sc.Labels).To(HaveKeyWithValue("team", "storage"))
		Expect(sc.Labels).To(HaveKeyWithValue(controller.NFSStorageClassManagedLabelKey, controller.NFSStorageClassManagedLabelValue))
	})

	It("propagates all NFSSC labels when no prefixes are provided", func() {
		nsc := newNsc("nfs-sc-no-filter", map[string]string{
			"argocd.argoproj.io/x": "1",
			"team":                 "storage",
		})

		sc := controller.ConfigureStorageClass(nil, nsc, controllerNamespace, nil)

		Expect(sc.Labels).To(HaveKeyWithValue("argocd.argoproj.io/x", "1"))
		Expect(sc.Labels).To(HaveKeyWithValue("team", "storage"))
		Expect(sc.Labels).To(HaveKeyWithValue(controller.NFSStorageClassManagedLabelKey, controller.NFSStorageClassManagedLabelValue))
	})
})
