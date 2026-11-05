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

// root-squash-probe runs in the CSI controller Pod and publishes the
// RootSquash condition of NFSStorageClasses.
//
// It lives there because it has to mount the export: the module controller
// runs unprivileged and the webhook answers synchronously, while the CSI
// controller Pod is privileged, runs in the host network next to the tlshd of
// the CSI node Pod that RPC-with-TLS handshakes go to, and ships the same
// mount.nfs the driver mounts the volumes with.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	apiruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/root-squash-probe/pkg/controller"
	"github.com/deckhouse/csi-nfs/images/root-squash-probe/pkg/probe"
	commonfeature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
	"github.com/deckhouse/sds-common-lib/logger"
)

const (
	namespaceEnv     = "POD_NAMESPACE"
	namespaceFile    = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	leaderElectionID = "csi-nfs-root-squash-probe"
)

func main() {
	var (
		interval       = flag.Duration("interval", 10*time.Minute, "how often the export of every NFSStorageClass is probed again")
		mountTimeout   = flag.Duration("mount-timeout", time.Minute, "how long a mount of the export may take")
		ioTimeout      = flag.Duration("io-timeout", time.Minute, "how long the file operations on the mounted export may take")
		unmountTimeout = flag.Duration("unmount-timeout", 30*time.Second, "how long a regular unmount may take before it is forced")
		workDir        = flag.String("work-dir", "/tmp", "directory for the local mount points; an emptyDir of this container")
		concurrency    = flag.Int("concurrency", 4, "how many exports are probed at once")
	)
	flag.Parse()

	log, err := logger.NewLoggerFromEnv(logger.InfoLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create a logger: %v\n", err)
		os.Exit(1)
	}

	namespace, err := podNamespace()
	if err != nil {
		log.Error("unable to tell the namespace of the Pod", logger.Err(err))
		os.Exit(1)
	}

	scheme := apiruntime.NewScheme()
	for _, add := range []func(*apiruntime.Scheme) error{clientgoscheme.AddToScheme, v1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			log.Error("unable to build the scheme", logger.Err(err))
			os.Exit(1)
		}
	}

	restCfg, err := ctrl.GetConfig()
	if err != nil {
		log.Error("unable to load the Kubernetes client configuration", logger.Err(err))
		os.Exit(1)
	}

	// The Pod runs in the host network, so neither metrics nor health probes
	// are served: a listener would take a port on the node.
	mgr, err := ctrl.NewManager(restCfg, manager.Options{
		Scheme:                        scheme,
		Logger:                        log.GetLogger(),
		Metrics:                       metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress:        "0",
		LeaderElection:                true,
		LeaderElectionID:              leaderElectionID,
		LeaderElectionNamespace:       namespace,
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		log.Error("unable to create the manager", logger.Err(err))
		os.Exit(1)
	}

	prober := &probe.Prober{
		Mounter:        probe.ExecMounter{},
		FS:             probe.OSFilesystem{},
		WorkDir:        *workDir,
		TLSEnabled:     commonfeature.TLSEnabled(),
		MountTimeout:   *mountTimeout,
		IOTimeout:      *ioTimeout,
		UnmountTimeout: *unmountTimeout,
		Log:            log.Named("probe"),
	}
	prober.Sweep()

	reconciler := &controller.Reconciler{
		Client:    mgr.GetClient(),
		APIReader: mgr.GetAPIReader(),
		Recorder:  mgr.GetEventRecorderFor(leaderElectionID),
		Prober:    prober,
		Interval:  *interval,
		Log:       log.Named(controller.Name),
		Now:       time.Now,
	}
	if err := reconciler.SetupWithManager(mgr, *concurrency); err != nil {
		log.Error("unable to set up the controller", logger.Err(err))
		os.Exit(1)
	}

	log.Info("starting", "interval", interval.String(), "tlsEnabled", commonfeature.TLSEnabled())
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error("the manager stopped", logger.Err(err))
		os.Exit(1)
	}
}

func podNamespace() (string, error) {
	if ns := os.Getenv(namespaceEnv); ns != "" {
		return ns, nil
	}
	b, err := os.ReadFile(namespaceFile)
	if err != nil {
		return "", fmt.Errorf("%s is not set and %w", namespaceEnv, err)
	}
	return strings.TrimSpace(string(b)), nil
}
