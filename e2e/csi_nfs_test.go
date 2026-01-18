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

package csi_nfs

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"fox.flant.com/deckhouse/storage/csi-nfs/e2e/helpers"
	"github.com/deckhouse/storage-e2e/pkg/cluster"
	"github.com/deckhouse/storage-e2e/pkg/config"
	"github.com/deckhouse/storage-e2e/pkg/kubernetes"
	"github.com/deckhouse/storage-e2e/pkg/testkit"
)

var _ = Describe("CSI NFS", Ordered, func() {
	var (
		testClusterResources *cluster.TestClusterResources
		nfsServers           map[string]*helpers.NFSServerInfo
	)

	// NFS server namespace
	nfsServerNamespace := "nfs-servers-e2e"

	BeforeAll(func() {
		By("Outputting environment variables", func() {
			testkit.OutputEnvironmentVariables()
		})
	})

	AfterAll(func() {
		// Cleanup NFS servers
		if testClusterResources != nil && nfsServers != nil {
			ctx := context.Background()
			By("Cleaning up NFS servers", func() {
				GinkgoWriter.Printf("    ▶️ Cleaning up NFS servers...\n")
				err := helpers.DeleteNFSServers(ctx, testClusterResources.Kubeconfig, nfsServerNamespace)
				if err != nil {
					GinkgoWriter.Printf("    ⚠️ Warning: Failed to cleanup NFS servers: %v\n", err)
				} else {
					GinkgoWriter.Printf("    ✅ NFS servers cleaned up\n")
				}
			})
		}

		testkit.CleanupTestClusterResources(testClusterResources)
	})

	// ---=== TEST CLUSTER IS CREATED OR CONNECTED HERE ===--- //

	It("should create or connect to test cluster and wait for it to become ready", func() {
		const maxRetries = 3
		const retryDelay = 30 * time.Second

		var lastErr error
		for attempt := 1; attempt <= maxRetries; attempt++ {
			GinkgoWriter.Printf("    ▶️ Attempt %d/%d to connect to test cluster...\n", attempt, maxRetries)

			// Recover from any panic that might occur during cluster connection
			func() {
				defer func() {
					if r := recover(); r != nil {
						lastErr = fmt.Errorf("panic during cluster connection: %v", r)
						GinkgoWriter.Printf("    ⚠️ Attempt %d failed with panic: %v\n", attempt, r)
					}
				}()
				testClusterResources = testkit.CreateOrConnectToTestCluster()
				lastErr = nil
			}()

			if lastErr == nil && testClusterResources != nil {
				GinkgoWriter.Printf("    ✅ Successfully connected to test cluster on attempt %d\n", attempt)
				return
			}

			if attempt < maxRetries {
				GinkgoWriter.Printf("    ⚠️ Attempt %d failed, waiting %v before retry...\n", attempt, retryDelay)
				time.Sleep(retryDelay)
			}
		}

		// If we get here, all retries failed
		Expect(lastErr).NotTo(HaveOccurred(), "Failed to connect to test cluster after %d attempts", maxRetries)
		Expect(testClusterResources).NotTo(BeNil(), "testClusterResources is nil after %d attempts", maxRetries)
	})

	////////////////////////////////////
	// ---=== TESTS START HERE ===--- //
	////////////////////////////////////

	// Storage class names with random suffix
	randomSuffix := testkit.GenerateRandomSuffix(6)

	It("should enable csi-nfs module with dependencies", func() {
		ctx := context.Background()

		// Ensure testClusterResources is not nil (previous test must have set it)
		Expect(testClusterResources).NotTo(BeNil(), "testClusterResources must be set by previous test")

		By("Waiting for Deckhouse webhook to be ready", func() {
			GinkgoWriter.Printf("    ▶️ Waiting for Deckhouse webhook to be ready...\n")
			webhookTimeout := 5 * time.Minute
			err := kubernetes.WaitForDeckhouseWebhookReady(
				ctx,
				testClusterResources.Kubeconfig,
				testClusterResources.SSHClient,
				webhookTimeout,
			)
			Expect(err).NotTo(HaveOccurred(), "Deckhouse webhook is not ready")
			GinkgoWriter.Printf("    ✅ Deckhouse webhook is ready\n")
		})

		By("Enabling snapshot-controller and csi-nfs modules", func() {
			GinkgoWriter.Printf("    ▶️ Enabling modules: snapshot-controller and csi-nfs...\n")

			// Define modules to enable
			modulesToEnable := []*config.ModuleConfig{
				{
					Name:               "snapshot-controller",
					Version:            1,
					Enabled:            true,
					Settings:           map[string]interface{}{},
					Dependencies:       []string{},
					ModulePullOverride: "main",
				},
				{
					Name:               "csi-nfs",
					Version:            1,
					Enabled:            true,
					Settings:           map[string]interface{}{},
					Dependencies:       []string{"snapshot-controller"},
					ModulePullOverride: "main",
				},
			}

			// Get registry repo - from ClusterDefinition if available (new cluster mode),
			// otherwise use default value (existing cluster mode where ClusterDefinition is nil)
			registryRepo := "dev-registry.deckhouse.io/sys/deckhouse-oss"
			if testClusterResources.ClusterDefinition != nil {
				registryRepo = testClusterResources.ClusterDefinition.DKPParameters.RegistryRepo
			}

			// Create cluster definition with modules to enable
			clusterDef := &config.ClusterDefinition{
				DKPParameters: config.DKPParameters{
					Modules:      modulesToEnable,
					RegistryRepo: registryRepo,
				},
			}

			// Enable and configure modules
			err := kubernetes.EnableAndConfigureModules(
				ctx,
				testClusterResources.Kubeconfig,
				clusterDef,
				testClusterResources.SSHClient,
			)
			Expect(err).NotTo(HaveOccurred(), "Failed to enable modules")

			// Wait for modules to become ready
			timeout := 10 * time.Minute
			err = kubernetes.WaitForModulesReady(
				ctx,
				testClusterResources.Kubeconfig,
				clusterDef,
				timeout,
			)
			Expect(err).NotTo(HaveOccurred(), "Failed waiting for modules to be ready")

			GinkgoWriter.Printf("    ✅ Modules enabled successfully\n")
		})

		By("Waiting for all pods in module namespaces to be ready", func() {
			GinkgoWriter.Printf("    ▶️ Waiting for all pods to be ready in module namespaces...\n")

			namespacesToWait := []string{
				"d8-snapshot-controller",
				"d8-csi-nfs",
			}

			podReadyTimeout := 10 * time.Minute
			for _, ns := range namespacesToWait {
				GinkgoWriter.Printf("      ▶️ Waiting for pods in namespace %s...\n", ns)
				err := testkit.WaitForAllPodsReadyInNamespace(ctx, testClusterResources.Kubeconfig, ns, podReadyTimeout)
				Expect(err).NotTo(HaveOccurred(), "Failed waiting for pods in namespace %s to be ready", ns)
				GinkgoWriter.Printf("      ✅ All pods in namespace %s are ready\n", ns)
			}

			GinkgoWriter.Printf("    ✅ All pods in module namespaces are ready\n")
		})

		By("Waiting additional 30 seconds for stabilization", func() {
			GinkgoWriter.Printf("    ▶️ Waiting 30 seconds for stabilization...\n")
			time.Sleep(30 * time.Second)
			GinkgoWriter.Printf("    ✅ Stabilization wait completed\n")
		})
	})

	It("should deploy NFS servers for versions 3, 4.1, and 4.2", func() {
		ctx := context.Background()

		By("Deploying NFS servers", func() {
			GinkgoWriter.Printf("    ▶️ Deploying NFS servers (v3, v4.1, v4.2) in namespace %s...\n", nfsServerNamespace)

			var err error
			nfsServers, err = helpers.DeployNFSServers(ctx, testClusterResources.Kubeconfig, nfsServerNamespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to deploy NFS servers")

			for version, serverInfo := range nfsServers {
				GinkgoWriter.Printf("      ✅ NFS v%s server deployed: %s (IP: %s)\n",
					version, serverInfo.Name, serverInfo.ServiceIP)
			}

			GinkgoWriter.Printf("    ✅ All NFS servers deployed successfully\n")
		})

		By("Waiting for NFS servers to be fully ready", func() {
			GinkgoWriter.Printf("    ▶️ Waiting 30 seconds for NFS servers to stabilize...\n")
			time.Sleep(30 * time.Second)
			GinkgoWriter.Printf("    ✅ NFS servers ready\n")
		})
	})

	// Test NFS v3
	It("should create NFSStorageClass for NFS v3 and run stress test", func() {
		ctx := context.Background()
		nfsVersion := "3"
		storageClassName := "nfs-v3-sc-" + randomSuffix

		Expect(nfsServers).NotTo(BeNil(), "NFS servers must be deployed before this test")
		serverInfo, ok := nfsServers[nfsVersion]
		Expect(ok).To(BeTrue(), "NFS v%s server must be available", nfsVersion)

		By("Creating NFSStorageClass for NFS v3", func() {
			GinkgoWriter.Printf("    ▶️ Creating NFSStorageClass: %s...\n", storageClassName)

			cfg := helpers.NFSStorageClassConfig{
				Name:              storageClassName,
				Host:              serverInfo.ServiceIP,
				Share:             serverInfo.SharePath,
				NFSVersion:        nfsVersion,
				ReclaimPolicy:     "Delete",
				VolumeBindingMode: "Immediate",
			}

			err := helpers.CreateNFSStorageClass(ctx, testClusterResources.Kubeconfig, cfg)
			Expect(err).NotTo(HaveOccurred(), "Failed to create NFSStorageClass")

			GinkgoWriter.Printf("    ✅ NFSStorageClass created: %s\n", storageClassName)
		})

		By("Waiting for NFSStorageClass to become ready", func() {
			GinkgoWriter.Printf("    ▶️ Waiting for NFSStorageClass to become ready...\n")

			err := helpers.WaitForNFSStorageClassCreated(ctx, testClusterResources.Kubeconfig, storageClassName, 5*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "NFSStorageClass did not become ready")

			GinkgoWriter.Printf("    ✅ NFSStorageClass is ready: %s\n", storageClassName)
		})

		By("Waiting for Kubernetes StorageClass to become available", func() {
			GinkgoWriter.Printf("    ▶️ Waiting for StorageClass %s...\n", storageClassName)
			err := kubernetes.WaitForStorageClass(ctx, testClusterResources.Kubeconfig, storageClassName, 10*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "StorageClass %s not available", storageClassName)
			GinkgoWriter.Printf("    ✅ StorageClass %s is available\n", storageClassName)
		})

		By("Running flog stress test with NFS v3 storage class", func() {
			GinkgoWriter.Printf("    ▶️ Running flog stress test with NFS v3 storage class: %s...\n", storageClassName)

			testCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			stressConfig := testkit.DefaultConfig()
			stressConfig.Namespace = "stress-test-flog-nfs-v3"
			stressConfig.StorageClassName = storageClassName
			stressConfig.Mode = testkit.ModeFlog

			runner, err := testkit.NewStressTestRunner(stressConfig, testClusterResources.Kubeconfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to create stress test runner")

			err = runner.Run(testCtx)
			Expect(err).NotTo(HaveOccurred(), "Stress test failed")

			GinkgoWriter.Printf("    ✅ Stress test with NFS v3 completed successfully\n")
		})

		By("Cleaning up NFSStorageClass for NFS v3", func() {
			GinkgoWriter.Printf("    ▶️ Deleting NFSStorageClass: %s...\n", storageClassName)
			err := helpers.DeleteNFSStorageClass(ctx, testClusterResources.Kubeconfig, storageClassName)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete NFSStorageClass")

			err = helpers.WaitForNFSStorageClassDeletion(ctx, testClusterResources.Kubeconfig, storageClassName, 5*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "NFSStorageClass was not deleted")
			GinkgoWriter.Printf("    ✅ NFSStorageClass deleted: %s\n", storageClassName)
		})
	})

	// Test NFS v4.1
	It("should create NFSStorageClass for NFS v4.1 and run stress test", func() {
		ctx := context.Background()
		nfsVersion := "4.1"
		storageClassName := "nfs-v4-1-sc-" + randomSuffix

		Expect(nfsServers).NotTo(BeNil(), "NFS servers must be deployed before this test")
		serverInfo, ok := nfsServers[nfsVersion]
		Expect(ok).To(BeTrue(), "NFS v%s server must be available", nfsVersion)

		By("Creating NFSStorageClass for NFS v4.1", func() {
			GinkgoWriter.Printf("    ▶️ Creating NFSStorageClass: %s...\n", storageClassName)

			cfg := helpers.NFSStorageClassConfig{
				Name:              storageClassName,
				Host:              serverInfo.ServiceIP,
				Share:             serverInfo.SharePath,
				NFSVersion:        nfsVersion,
				ReclaimPolicy:     "Delete",
				VolumeBindingMode: "Immediate",
			}

			err := helpers.CreateNFSStorageClass(ctx, testClusterResources.Kubeconfig, cfg)
			Expect(err).NotTo(HaveOccurred(), "Failed to create NFSStorageClass")

			GinkgoWriter.Printf("    ✅ NFSStorageClass created: %s\n", storageClassName)
		})

		By("Waiting for NFSStorageClass to become ready", func() {
			GinkgoWriter.Printf("    ▶️ Waiting for NFSStorageClass to become ready...\n")

			err := helpers.WaitForNFSStorageClassCreated(ctx, testClusterResources.Kubeconfig, storageClassName, 5*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "NFSStorageClass did not become ready")

			GinkgoWriter.Printf("    ✅ NFSStorageClass is ready: %s\n", storageClassName)
		})

		By("Waiting for Kubernetes StorageClass to become available", func() {
			GinkgoWriter.Printf("    ▶️ Waiting for StorageClass %s...\n", storageClassName)
			err := kubernetes.WaitForStorageClass(ctx, testClusterResources.Kubeconfig, storageClassName, 10*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "StorageClass %s not available", storageClassName)
			GinkgoWriter.Printf("    ✅ StorageClass %s is available\n", storageClassName)
		})

		By("Running flog stress test with NFS v4.1 storage class", func() {
			GinkgoWriter.Printf("    ▶️ Running flog stress test with NFS v4.1 storage class: %s...\n", storageClassName)

			testCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			stressConfig := testkit.DefaultConfig()
			stressConfig.Namespace = "stress-test-flog-nfs-v4-1"
			stressConfig.StorageClassName = storageClassName
			stressConfig.Mode = testkit.ModeFlog

			runner, err := testkit.NewStressTestRunner(stressConfig, testClusterResources.Kubeconfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to create stress test runner")

			err = runner.Run(testCtx)
			Expect(err).NotTo(HaveOccurred(), "Stress test failed")

			GinkgoWriter.Printf("    ✅ Stress test with NFS v4.1 completed successfully\n")
		})

		By("Cleaning up NFSStorageClass for NFS v4.1", func() {
			GinkgoWriter.Printf("    ▶️ Deleting NFSStorageClass: %s...\n", storageClassName)
			err := helpers.DeleteNFSStorageClass(ctx, testClusterResources.Kubeconfig, storageClassName)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete NFSStorageClass")

			err = helpers.WaitForNFSStorageClassDeletion(ctx, testClusterResources.Kubeconfig, storageClassName, 5*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "NFSStorageClass was not deleted")
			GinkgoWriter.Printf("    ✅ NFSStorageClass deleted: %s\n", storageClassName)
		})
	})

	// Test NFS v4.2
	It("should create NFSStorageClass for NFS v4.2 and run stress test", func() {
		ctx := context.Background()
		nfsVersion := "4.2"
		storageClassName := "nfs-v4-2-sc-" + randomSuffix

		Expect(nfsServers).NotTo(BeNil(), "NFS servers must be deployed before this test")
		serverInfo, ok := nfsServers[nfsVersion]
		Expect(ok).To(BeTrue(), "NFS v%s server must be available", nfsVersion)

		By("Creating NFSStorageClass for NFS v4.2", func() {
			GinkgoWriter.Printf("    ▶️ Creating NFSStorageClass: %s...\n", storageClassName)

			cfg := helpers.NFSStorageClassConfig{
				Name:              storageClassName,
				Host:              serverInfo.ServiceIP,
				Share:             serverInfo.SharePath,
				NFSVersion:        nfsVersion,
				ReclaimPolicy:     "Delete",
				VolumeBindingMode: "Immediate",
			}

			err := helpers.CreateNFSStorageClass(ctx, testClusterResources.Kubeconfig, cfg)
			Expect(err).NotTo(HaveOccurred(), "Failed to create NFSStorageClass")

			GinkgoWriter.Printf("    ✅ NFSStorageClass created: %s\n", storageClassName)
		})

		By("Waiting for NFSStorageClass to become ready", func() {
			GinkgoWriter.Printf("    ▶️ Waiting for NFSStorageClass to become ready...\n")

			err := helpers.WaitForNFSStorageClassCreated(ctx, testClusterResources.Kubeconfig, storageClassName, 5*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "NFSStorageClass did not become ready")

			GinkgoWriter.Printf("    ✅ NFSStorageClass is ready: %s\n", storageClassName)
		})

		By("Waiting for Kubernetes StorageClass to become available", func() {
			GinkgoWriter.Printf("    ▶️ Waiting for StorageClass %s...\n", storageClassName)
			err := kubernetes.WaitForStorageClass(ctx, testClusterResources.Kubeconfig, storageClassName, 10*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "StorageClass %s not available", storageClassName)
			GinkgoWriter.Printf("    ✅ StorageClass %s is available\n", storageClassName)
		})

		By("Running flog stress test with NFS v4.2 storage class", func() {
			GinkgoWriter.Printf("    ▶️ Running flog stress test with NFS v4.2 storage class: %s...\n", storageClassName)

			testCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			stressConfig := testkit.DefaultConfig()
			stressConfig.Namespace = "stress-test-flog-nfs-v4-2"
			stressConfig.StorageClassName = storageClassName
			stressConfig.Mode = testkit.ModeFlog

			runner, err := testkit.NewStressTestRunner(stressConfig, testClusterResources.Kubeconfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to create stress test runner")

			err = runner.Run(testCtx)
			Expect(err).NotTo(HaveOccurred(), "Stress test failed")

			GinkgoWriter.Printf("    ✅ Stress test with NFS v4.2 completed successfully\n")
		})

		By("Running snapshot/resize/clone stress test with NFS v4.2", func() {
			GinkgoWriter.Printf("    ▶️ Running complex stress test with NFS v4.2 storage class: %s...\n", storageClassName)

			testCtx, cancel := context.WithTimeout(ctx, 45*time.Minute)
			defer cancel()

			stressConfig := testkit.DefaultConfig()
			stressConfig.Namespace = "stress-test-complex-nfs-v4-2"
			stressConfig.StorageClassName = storageClassName
			stressConfig.Mode = testkit.ModeSnapshotResizeCloning

			runner, err := testkit.NewStressTestRunner(stressConfig, testClusterResources.Kubeconfig)
			Expect(err).NotTo(HaveOccurred(), "Failed to create stress test runner")

			err = runner.Run(testCtx)
			Expect(err).NotTo(HaveOccurred(), "Complex stress test failed")

			GinkgoWriter.Printf("    ✅ Complex stress test with NFS v4.2 completed successfully\n")
		})

		By("Cleaning up NFSStorageClass for NFS v4.2", func() {
			GinkgoWriter.Printf("    ▶️ Deleting NFSStorageClass: %s...\n", storageClassName)
			err := helpers.DeleteNFSStorageClass(ctx, testClusterResources.Kubeconfig, storageClassName)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete NFSStorageClass")

			err = helpers.WaitForNFSStorageClassDeletion(ctx, testClusterResources.Kubeconfig, storageClassName, 5*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "NFSStorageClass was not deleted")
			GinkgoWriter.Printf("    ✅ NFSStorageClass deleted: %s\n", storageClassName)
		})
	})

	It("should cleanup NFS servers", func() {
		ctx := context.Background()

		By("Deleting NFS servers", func() {
			GinkgoWriter.Printf("    ▶️ Deleting NFS servers from namespace %s...\n", nfsServerNamespace)

			err := helpers.DeleteNFSServers(ctx, testClusterResources.Kubeconfig, nfsServerNamespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete NFS servers")

			GinkgoWriter.Printf("    ✅ NFS servers deleted\n")
		})
	})

	///////////////////////////////////////////////////// ---=== TESTS END HERE ===--- /////////////////////////////////////////////////////

}) // Describe: CSI NFS
