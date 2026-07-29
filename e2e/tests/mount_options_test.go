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

package tests

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/deckhouse/csi-nfs/e2e/framework"
)

// mountOptions travel two ways out of an NFSStorageClass: onto the StorageClass
// the workload mounts with, and into the Secret the provisioner and deleter use to
// mount the share on their own. Both must agree, so this scenario checks both.
var _ = Describe("csi-nfs StorageClass mount options", Label("csi-nfs", "mount-options"), Ordered, func() {
	var (
		env *suiteEnv
		srv framework.Server

		scName = "e2e-mountopts"
		opts   framework.NSCOptions
	)

	dumpDiagnosticsOnFailure(&env)

	BeforeAll(func() {
		conf := mustLoadConfig()
		srv = serverV42(conf)
		requireServer(srv)

		env = connectSuite("mount-options", conf)

		readOnly := true
		opts = framework.NSCOptions{
			MountMode:       "soft",
			Timeout:         300,
			Retransmissions: 5,
			ReadOnly:        &readOnly,
		}

		DeferCleanup(func(ctx context.Context) {
			_ = framework.DeleteNFSStorageClass(ctx, env.Dynamic, env.Client, scName, resourceGoneTimeout)
		})

		Expect(framework.CreateNFSStorageClass(env.Ctx, env.Dynamic,
			framework.BuildNFSStorageClass(scName, srv, opts))).To(Succeed())
		Expect(framework.WaitNSCCreated(env.Ctx, env.Dynamic, env.Client, scName, nscReadyTimeout)).To(Succeed())
	})

	It("renders the options onto the StorageClass", func() {
		want := framework.ExpectedMountOptions(srv, opts)
		sc, err := framework.GetStorageClass(env.Ctx, env.Client, scName)
		Expect(err).NotTo(HaveOccurred())
		Expect(framework.ContainsAll(sc.MountOptions, want)).
			To(BeTrue(), "StorageClass mountOptions %v should contain %v", sc.MountOptions, want)
	})

	It("mirrors the same options into the provisioning Secret", func() {
		want := framework.ExpectedMountOptions(srv, opts)
		sc, err := framework.GetStorageClass(env.Ctx, env.Client, scName)
		Expect(err).NotTo(HaveOccurred())

		secret, err := framework.GetMountOptionsSecret(env.Ctx, env.Client, sc)
		Expect(err).NotTo(HaveOccurred())

		var rendered string
		for _, v := range secret.Data {
			rendered = string(v)
		}
		for _, opt := range want {
			Expect(rendered).To(ContainSubstring(opt),
				"the mount-options Secret %s/%s should carry %q", secret.Namespace, secret.Name, opt)
		}
	})
})
