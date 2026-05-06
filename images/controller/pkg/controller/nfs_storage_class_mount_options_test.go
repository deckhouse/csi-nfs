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

package controller_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/controller/pkg/controller"
)

var _ = Describe("GetSCMountOptions", func() {
	boolPtr := func(b bool) *bool { return &b }

	It("emits nolock for NFSv3 when MountOptions.Nolock is true", func() {
		nsc := &v1alpha1.NFSStorageClass{
			Spec: v1alpha1.NFSStorageClassSpec{
				Connection: &v1alpha1.NFSStorageClassConnection{
					Host:       "10.0.0.1",
					Share:      "/data",
					NFSVersion: "3",
				},
				MountOptions: &v1alpha1.NFSStorageClassMountOptions{
					Nolock: boolPtr(true),
				},
			},
		}

		Expect(controller.GetSCMountOptions(nsc)).To(ContainElement("nolock"))
	})

	It("does not emit nolock when MountOptions.Nolock is false", func() {
		nsc := &v1alpha1.NFSStorageClass{
			Spec: v1alpha1.NFSStorageClassSpec{
				Connection: &v1alpha1.NFSStorageClassConnection{
					Host:       "10.0.0.1",
					Share:      "/data",
					NFSVersion: "3",
				},
				MountOptions: &v1alpha1.NFSStorageClassMountOptions{
					Nolock: boolPtr(false),
				},
			},
		}

		Expect(controller.GetSCMountOptions(nsc)).NotTo(ContainElement("nolock"))
	})

	It("does not emit nolock when MountOptions.Nolock is unset", func() {
		nsc := &v1alpha1.NFSStorageClass{
			Spec: v1alpha1.NFSStorageClassSpec{
				Connection: &v1alpha1.NFSStorageClassConnection{
					Host:       "10.0.0.1",
					Share:      "/data",
					NFSVersion: "3",
				},
				MountOptions: &v1alpha1.NFSStorageClassMountOptions{},
			},
		}

		Expect(controller.GetSCMountOptions(nsc)).NotTo(ContainElement("nolock"))
	})

	DescribeTable("does not emit nolock for non-v3 versions even if Nolock=true (defence-in-depth, CRD CEL also blocks this)",
		func(version string) {
			nsc := &v1alpha1.NFSStorageClass{
				Spec: v1alpha1.NFSStorageClassSpec{
					Connection: &v1alpha1.NFSStorageClassConnection{
						Host:       "10.0.0.1",
						Share:      "/data",
						NFSVersion: version,
					},
					MountOptions: &v1alpha1.NFSStorageClassMountOptions{
						Nolock: boolPtr(true),
					},
				},
			}

			Expect(controller.GetSCMountOptions(nsc)).NotTo(ContainElement("nolock"))
		},
		Entry("NFSv4.1", "4.1"),
		Entry("NFSv4.2", "4.2"),
	)
})
