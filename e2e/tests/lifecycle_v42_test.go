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
	. "github.com/onsi/ginkgo/v2"
)

// The full volume lifecycle over NFSv4.2, against the v4-only server. The
// server-side inspector is attached here, so this scenario checks what the driver
// writes to the export, not only what Kubernetes reports.
var _ = Describe("csi-nfs volume lifecycle over NFSv4.2", Label("csi-nfs", "lifecycle-v42"), Ordered, func() {
	var (
		env *suiteEnv
		lc  *lifecycle
	)

	dumpDiagnosticsOnFailure(&env)

	BeforeAll(func() {
		conf := mustLoadConfig()
		srv := serverV42(conf)
		requireServer(srv)

		env = connectSuite("lifecycle-v42", conf)

		lc = newLifecycle(env, srv)
		lc.inspector = newInspector(env, srv)
		DeferCleanup(lc.cleanup)
	})

	It("materialises a StorageClass from the NFSStorageClass", func() { lc.materialiseStorageClass() })
	It("creates a volume: the PVC binds and a Pod round-trips data", func() { lc.createVolume() })
	It("expands the volume and keeps the data intact", func() { lc.expandVolume() })
	It("migrates the Pod to another node and keeps the data intact", func() { lc.migratePod() })
	It("serves the same volume read-write from two nodes at once", func() { lc.serveRWXFromTwoNodes() })
	It("snapshots the volume and restores it into a new one", func() { lc.snapshotAndRestore() })
	It("clones the volume from the source PVC", func() { lc.cloneFromPVC() })
	It("deletes the volumes and reclaims the share", func() { lc.deleteAndReclaim() })
})
