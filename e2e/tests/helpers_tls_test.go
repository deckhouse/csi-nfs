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
	"errors"
	"fmt"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/deckhouse/csi-nfs/e2e/framework"
)

// Shared between the tls and mtls scenarios: enablement is cluster-wide, so the
// first to run pays for it.
var tlsOnce struct {
	sync.Once
	skip string
	err  error
}

// Lazily, not in suite setup - see framework.EnableTLS.
func requireRPCWithTLS(env *suiteEnv) {
	GinkgoHelper()

	tlsOnce.Do(func() {
		if !env.Conf.NFS.TLSMaterialPresent() {
			tlsOnce.skip = "no RPC-with-TLS material supplied (E2E_NFS_TLS_CA is empty)"
			return
		}

		By("enabling RPC-with-TLS on the csi-nfs ModuleConfig")
		err := framework.EnableTLS(env.Ctx, env.Dynamic, env.RESTCfg, framework.TLSParameters{
			CA:         env.Conf.NFS.TLSCA,
			ClientCert: env.Conf.NFS.TLSClientCert,
			ClientKey:  env.Conf.NFS.TLSClientKey,
		}, env.Conf.Timeouts.ModuleReady)
		switch {
		case errors.Is(err, framework.ErrTLSFeatureUnavailable):
			tlsOnce.skip = "this edition has no RPC-with-TLS feature"
			return
		case err != nil:
			tlsOnce.err = err
			return
		}

		By("waiting for the CSI node DaemonSet to roll out with the tlshd sidecar")
		if err := framework.WaitTLSRollout(env.Ctx, env.Client, tlshdRenderTimeout, env.Conf.Timeouts.TLSRollout); err != nil {
			framework.DumpCSINodeDaemonSet(env.Ctx, GinkgoWriter, env.Client)
			tlsOnce.err = err
		}
	})

	if tlsOnce.skip != "" {
		Skip(tlsOnce.skip)
	}
	if tlsOnce.err != nil {
		Fail(fmt.Sprintf("the csi-nfs node DaemonSet never became Ready with the tlshd sidecar, "+
			"so no RPC-with-TLS mount can succeed: %v", tlsOnce.err))
	}
}

// Guards a stand misconfiguration that looks like a driver bug: the kernel NFS
// client keys its transport by server address, so one address cannot serve tls and
// mtls at once.
func requireDistinctMTLSHost(env *suiteEnv) {
	GinkgoHelper()
	if env.Conf.NFS.MTLSCollidesWithTLS() {
		Skip(fmt.Sprintf("E2E_NFS_MTLS_HOST and E2E_NFS_TLS_HOST are the same host (%s); "+
			"one address cannot serve both security modes at once - give mtls its own server",
			env.Conf.NFS.MTLSHost))
	}
}

func newInspector(env *suiteEnv, srv framework.Server) *framework.Inspector {
	GinkgoHelper()

	insp, err := framework.NewInspector(env.Ctx, env.Client, env.RESTCfg, env.Namespace(),
		srv, env.Conf.Workload.ProbeImage, pvcBindTimeout, podReadyTimeout)
	Expect(err).NotTo(HaveOccurred(), "start the inspector for %s", srv)
	DeferCleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), resourceGoneTimeout)
		defer cancel()
		if closeErr := insp.Close(ctx, resourceGoneTimeout); closeErr != nil {
			GinkgoWriter.Printf("  warning: closing the inspector: %v\n", closeErr)
		}
	})
	return insp
}
