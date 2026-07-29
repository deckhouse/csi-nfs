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

// Package cfg carries the csi-nfs e2e configuration. csi-nfs only connects to an
// NFS server, so the suite is parameterised by external servers.
package cfg

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	TestCluster TestCluster
	NFS         NFS
	Workload    Workload
	Timeouts    Timeouts
}

type TestCluster struct {
	// Same variable the provider layer uses, so both stay in step.
	Namespace string `env:"TEST_CLUSTER_NAMESPACE" envDefault:"e2e-csi-nfs"`
}

// NFS is the external stand. Every server is optional: a scenario whose server is
// unset skips itself. Separate hosts per version so a mismatch fails loudly instead
// of negotiating down.
type NFS struct {
	V3Host  string `env:"E2E_NFS_V3_HOST"`
	V3Share string `env:"E2E_NFS_V3_SHARE" envDefault:"/srv/nfs/e2e"`
	V4Host  string `env:"E2E_NFS_V4_HOST"`
	V4Share string `env:"E2E_NFS_V4_SHARE" envDefault:"/srv/nfs/e2e"`

	// A second export for scenarios that leave data behind (Retain).
	V4ShareAlt string `env:"E2E_NFS_V4_SHARE_ALT" envDefault:"/srv/nfs/e2e-alt"`

	// Must be DIFFERENT hosts: the kernel NFS client keys its transport by server
	// address, so one address cannot serve both security modes at once.
	TLSHost   string `env:"E2E_NFS_TLS_HOST"`
	TLSShare  string `env:"E2E_NFS_TLS_SHARE" envDefault:"/srv/nfs/tls"`
	MTLSHost  string `env:"E2E_NFS_MTLS_HOST"`
	MTLSShare string `env:"E2E_NFS_MTLS_SHARE" envDefault:"/srv/nfs/mtls"`

	// Base64-encoded PEM for the ModuleConfig tlsParameters.
	TLSCA         string `env:"E2E_NFS_TLS_CA"`
	TLSClientCert string `env:"E2E_NFS_TLS_CLIENT_CERT"`
	TLSClientKey  string `env:"E2E_NFS_TLS_CLIENT_KEY"`
}

type Workload struct {
	PVCSize    string `env:"E2E_PVC_SIZE" envDefault:"1Gi"`
	ProbeImage string `env:"E2E_PROBE_IMAGE" envDefault:"busybox:1.36"` // needs sh, cat, stat
}

type Timeouts struct {
	ModuleReady time.Duration `env:"E2E_MODULE_READY_TIMEOUT" envDefault:"15m"`
	// tlsParameters recreate every node Pod, one node at a time.
	TLSRollout time.Duration `env:"E2E_TLS_ROLLOUT_TIMEOUT" envDefault:"15m"`
}

func Load() (*Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, fmt.Errorf("parse e2e config from environment: %w", err)
	}
	return &c, nil
}

// The CA is all a plain TLS mount needs.
func (n NFS) TLSMaterialPresent() bool { return n.TLSCA != "" }

func (n NFS) MTLSMaterialPresent() bool {
	return n.TLSCA != "" && n.TLSClientCert != "" && n.TLSClientKey != ""
}

// Both TLS scenarios aiming at one address cannot work - see TLSHost.
func (n NFS) MTLSCollidesWithTLS() bool {
	return n.MTLSHost != "" && n.MTLSHost == n.TLSHost
}
