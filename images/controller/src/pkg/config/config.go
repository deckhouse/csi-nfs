/*
Copyright 2024 Flant JSC
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
	"d8-controller/pkg/logger"
	"log"
	"os"
	"time"
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
)

type Options struct {
	Loglevel                    logger.Verbosity
	RequeueStorageClassInterval time.Duration
	RequeueModuleConfigInterval time.Duration
	HealthProbeBindAddress      string
	ControllerNamespace         string
	CsiNfsModuleName            string
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

	return &opts
}
