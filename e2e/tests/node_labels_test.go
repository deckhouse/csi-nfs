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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/deckhouse/csi-nfs/e2e/framework"
)

// The baseline the workloadNodes scenario narrows: with no NFSStorageClass
// restricting workloadNodes the controller's union of selectors is "every node", so
// every schedulable node must carry the csi-nfs node label and run the DaemonSet.
var _ = Describe("csi-nfs node labelling without a workloadNodes selector",
	Label("csi-nfs", "node-labels"), Ordered, func() {
		var (
			env         *suiteEnv
			schedulable []string
		)

		dumpDiagnosticsOnFailure(&env)

		BeforeAll(func() {
			conf := mustLoadConfig()
			env = connectSuite("node-labels", conf)

			var err error
			schedulable, err = framework.AllSchedulableNodes(env.Ctx, env.Client)
			Expect(err).NotTo(HaveOccurred())
			Expect(schedulable).NotTo(BeEmpty())
		})

		It("labels every schedulable node for csi-nfs", func() {
			Eventually(func() ([]string, error) {
				return framework.NodesMissingLabel(env.Ctx, env.Client, schedulable, framework.NodeLabelKey)
			}, 3*time.Minute, framework.PollInterval).Should(BeEmpty(),
				"every schedulable node should carry %s", framework.NodeLabelKey)
		})

		It("runs the CSI node DaemonSet on all of them", func() {
			Expect(framework.WaitCSINodeDaemonSetReady(env.Ctx, env.Client, 5*time.Minute)).To(Succeed())
		})
	})
