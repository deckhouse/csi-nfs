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

package config

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/deckhouse/csi-nfs/images/controller/pkg/logger"
)

const (
	LogLevelEnvName                      = "LOG_LEVEL"
	ControllerNamespaceEnv               = "CONTROLLER_NAMESPACE"
	HardcodedControllerNS                = "d8-csi-nfs"
	ControllerName                       = "d8-controller"
	DefaultHealthProbeBindAddressEnvName = "HEALTH_PROBE_BIND_ADDRESS"
	DefaultHealthProbeBindAddress        = ":8081"
	DefaultRequeueStorageClassInterval   = 10
	DefaultRequeueModuleConfigInterval   = 10
	CsiNfsModuleName                     = "csi-nfs"
	DefaultRequeueNodeSelectorInterval   = 10
	ConfigSecretName                     = "d8-csi-nfs-controller-config"
	// StorageClassLabelIgnoredPrefixesEnvName carries a comma-separated list of label-key
	// prefixes whose matching labels MUST NOT be propagated from an NFSStorageClass to
	// the managed Kubernetes StorageClass.
	StorageClassLabelIgnoredPrefixesEnvName = "STORAGE_CLASS_LABEL_IGNORED_PREFIXES"
)

type Options struct {
	Loglevel                    logger.Verbosity
	RequeueStorageClassInterval time.Duration
	RequeueModuleConfigInterval time.Duration
	RequeueNodeSelectorInterval time.Duration
	ConfigSecretName            string
	HealthProbeBindAddress      string
	ControllerNamespace         string
	CsiNfsModuleName            string
	// StorageClassLabelIgnoredPrefixes is the union of a system (hardcoded in Helm
	// internal values) and a user-configured (ModuleConfig) list of label-key prefixes.
	// Labels on an NFSStorageClass whose keys start with any of these prefixes are
	// NOT propagated to the managed Kubernetes StorageClass.
	StorageClassLabelIgnoredPrefixes []string
}

func NewConfig() *Options {
	var opts Options

	loglevel := os.Getenv(LogLevelEnvName)
	if loglevel == "" {
		opts.Loglevel = logger.DebugLevel
	} else {
		opts.Loglevel = logger.Verbosity(loglevel)
	}

	opts.HealthProbeBindAddress = os.Getenv(DefaultHealthProbeBindAddressEnvName)
	if opts.HealthProbeBindAddress == "" {
		opts.HealthProbeBindAddress = DefaultHealthProbeBindAddress
	}

	opts.ControllerNamespace = os.Getenv(ControllerNamespaceEnv)
	if opts.ControllerNamespace == "" {
		namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			log.Printf("Failed to get namespace from filesystem: %v", err)
			log.Printf("Using hardcoded namespace: %s", HardcodedControllerNS)
			opts.ControllerNamespace = HardcodedControllerNS
		} else {
			log.Printf("Got namespace from filesystem: %s", string(namespace))
			opts.ControllerNamespace = string(namespace)
		}
	}

	opts.RequeueStorageClassInterval = DefaultRequeueStorageClassInterval
	opts.RequeueModuleConfigInterval = DefaultRequeueModuleConfigInterval

	opts.CsiNfsModuleName = CsiNfsModuleName
	opts.RequeueNodeSelectorInterval = DefaultRequeueNodeSelectorInterval
	opts.ConfigSecretName = ConfigSecretName

	opts.StorageClassLabelIgnoredPrefixes = parseStorageClassLabelIgnoredPrefixes(os.Getenv(StorageClassLabelIgnoredPrefixesEnvName))

	return &opts
}

// parseStorageClassLabelIgnoredPrefixes parses a comma-separated env var value into
// a slice of non-empty, trimmed prefixes. Whitespace and empty entries are skipped so
// that a stray comma cannot match every label key via HasPrefix("", ...).
func parseStorageClassLabelIgnoredPrefixes(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

type CSINFSControllerConfig struct {
	NodeSelector map[string]string `yaml:"nodeSelector"`
}
