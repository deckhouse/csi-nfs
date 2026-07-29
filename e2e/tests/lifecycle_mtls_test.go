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

// The full volume lifecycle over mutual RPC-with-TLS (xprtsec=mtls), on a server of
// its own: once a node holds an mtls mount to an address, a tls mount to it is
// refused with "Operation not permitted" - hence the module docs' note that one
// server cannot serve several security modes at once.
var _ = Describe("csi-nfs volume lifecycle over RPC-with-mTLS", Label("csi-nfs", "lifecycle-mtls"), Ordered, func() {
	var (
		env *suiteEnv
		lc  *lifecycle
	)

	dumpDiagnosticsOnFailure(&env)

	BeforeAll(func() {
		conf := mustLoadConfig()
		srv := serverMTLS(conf)
		requireServer(srv)

		env = connectSuite("lifecycle-mtls", conf)
		requireDistinctMTLSHost(env)
		requireRPCWithTLS(env)

		lc = newLifecycle(env, srv)
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
