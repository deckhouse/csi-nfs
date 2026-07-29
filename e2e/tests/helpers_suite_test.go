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
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/deckhouse/csi-nfs/e2e/cfg"
	"github.com/deckhouse/csi-nfs/e2e/framework"
	"github.com/deckhouse/storage-e2e/pkg/e2e"
	storagekube "github.com/deckhouse/storage-e2e/pkg/kubernetes"
)

const (
	pvcBindTimeout       = 5 * time.Minute
	podReadyTimeout      = 5 * time.Minute
	nscReadyTimeout      = 5 * time.Minute
	resourceGoneTimeout  = 10 * time.Minute
	snapshotReadyTimeout = 10 * time.Minute
	tlshdRenderTimeout   = 5 * time.Minute
)

// Every scenario builds its own in BeforeAll, so scenarios stay individually
// runnable.
type suiteEnv struct {
	Ctx     context.Context
	Conf    *cfg.Config
	Cluster *e2e.Cluster
	RESTCfg *rest.Config
	Client  client.Client
	Dynamic dynamic.Interface
}

func (s *suiteEnv) Namespace() string { return s.Conf.TestCluster.Namespace }

// Side-effect free, so scenarios call it before connecting to decide whether they
// can run at all.
func mustLoadConfig() *cfg.Config {
	GinkgoHelper()
	conf, err := cfg.Load()
	Expect(err).NotTo(HaveOccurred(), "load e2e config")
	Expect(conf).NotTo(BeNil())
	return conf
}

// Closed through DeferCleanup, so the per-scenario lease is released even on
// failure. Cluster teardown belongs to the pipeline, not here.
func connectSuite(testName string, conf *cfg.Config) *suiteEnv {
	GinkgoHelper()

	env := &suiteEnv{Ctx: context.Background(), Conf: conf}

	var err error
	env.Cluster, err = e2e.Connect(env.Ctx, e2e.WithTestName(testName))
	Expect(err).NotTo(HaveOccurred(), "connect to the test cluster")
	DeferCleanup(func() {
		if closeErr := env.Cluster.Close(context.Background()); closeErr != nil {
			GinkgoWriter.Printf("  warning: closing the cluster handle: %v\n", closeErr)
		}
	})

	env.RESTCfg = env.Cluster.RESTConfig()
	env.Dynamic = env.Cluster.Dynamic()

	env.Client, err = client.New(env.RESTCfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred(), "build controller-runtime client")

	ensureClusterPrepared(env)

	return env
}

// Guards setup that belongs to the CLUSTER, not to a scenario: per scenario, a
// module that never becomes Ready would burn the readiness timeout ten times over.
// Not BeforeSuite, because scenarios connect on their own.
var clusterPreparedOnce struct {
	sync.Once
	err error
}

func ensureClusterPrepared(env *suiteEnv) {
	GinkgoHelper()

	clusterPreparedOnce.Do(func() {
		By("waiting for the csi-nfs module to become Ready")
		if err := framework.WaitModuleReady(env.Ctx, env.RESTCfg, env.Conf.Timeouts.ModuleReady); err != nil {
			clusterPreparedOnce.err = fmt.Errorf("csi-nfs module readiness: %w", err)
			return
		}
		if _, err := storagekube.CreateNamespaceIfNotExists(env.Ctx, env.RESTCfg, env.Namespace()); err != nil {
			clusterPreparedOnce.err = fmt.Errorf("ensure the test namespace %s: %w", env.Namespace(), err)
		}
	})

	Expect(clusterPreparedOnce.err).NotTo(HaveOccurred())
}

// MUST be called from the Describe body, not BeforeAll: registering a node from a
// leaf node is a spec-tree error. Hence **suiteEnv - at construction time only the
// variable exists, not the env.
func dumpDiagnosticsOnFailure(env **suiteEnv) {
	AfterEach(func() {
		if *env == nil || !CurrentSpecReport().Failed() {
			return
		}
		e := *env
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		framework.DumpDiagnostics(ctx, GinkgoWriter, e.Client, e.Dynamic, e.Namespace())
	})
}

// --- server specs from configuration ----------------------------------------

func serverV3(c *cfg.Config) framework.Server {
	return framework.Server{
		Key: "v3", Host: c.NFS.V3Host, Share: c.NFS.V3Share,
		NFSVersion: "3", Security: framework.SecurityPlain,
	}
}

func serverV41(c *cfg.Config) framework.Server {
	return framework.Server{
		Key: "v41", Host: c.NFS.V4Host, Share: c.NFS.V4Share,
		NFSVersion: "4.1", Security: framework.SecurityPlain,
	}
}

func serverV42(c *cfg.Config) framework.Server {
	return framework.Server{
		Key: "v42", Host: c.NFS.V4Host, Share: c.NFS.V4Share,
		NFSVersion: "4.2", Security: framework.SecurityPlain,
	}
}

// A second export, for scenarios that leave data behind.
func serverV4Alt(c *cfg.Config) framework.Server {
	return framework.Server{
		Key: "v42alt", Host: c.NFS.V4Host, Share: c.NFS.V4ShareAlt,
		NFSVersion: "4.2", Security: framework.SecurityPlain,
	}
}

func serverTLS(c *cfg.Config) framework.Server {
	return framework.Server{
		Key: "tls", Host: c.NFS.TLSHost, Share: c.NFS.TLSShare,
		NFSVersion: "4.2", Security: framework.SecurityTLS,
	}
}

func serverMTLS(c *cfg.Config) framework.Server {
	return framework.Server{
		Key: "mtls", Host: c.NFS.MTLSHost, Share: c.NFS.MTLSShare,
		NFSVersion: "4.2", Security: framework.SecurityMTLS,
	}
}

func requireServer(srv framework.Server) {
	GinkgoHelper()
	if !srv.Configured() {
		Skip("no NFS server configured for the " + srv.Key + " case")
	}
}
