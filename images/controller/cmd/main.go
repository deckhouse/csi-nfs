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

package main

import (
	"context"
	"fmt"
	"os"
	goruntime "runtime"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	v1 "k8s.io/api/core/v1"
	sv1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/controller/pkg/config"
	"github.com/deckhouse/csi-nfs/images/controller/pkg/controller"
	"github.com/deckhouse/csi-nfs/images/controller/pkg/kubutils"
	commonfeature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
	"github.com/deckhouse/sds-common-lib/logger"
)

var (
	resourcesSchemeFuncs = []func(*apiruntime.Scheme) error{
		cn.AddToScheme,
		clientgoscheme.AddToScheme,
		extv1.AddToScheme,
		v1.AddToScheme,
		sv1.AddToScheme,
		snapshotv1.AddToScheme,
	}
)

func main() {
	ctx := context.Background()
	cfgParams := config.NewConfig()

	baseLog, err := logger.NewLogger(cfgParams.Loglevel)
	if err != nil {
		fmt.Printf("unable to create NewLogger, err: %v\n", err)
		os.Exit(1)
	}
	log := baseLog.Named("main")

	log.Info("starting csi-nfs-controller",
		"goVersion", goruntime.Version(), "os", goruntime.GOOS, "arch", goruntime.GOARCH)
	log.Info("configuration",
		"logLevel", string(cfgParams.Loglevel),
		"tlsEnabled", commonfeature.TLSEnabled(),
		"requeueStorageClassInterval", cfgParams.RequeueStorageClassInterval.String(),
		"healthProbeBindAddress", cfgParams.HealthProbeBindAddress,
		"metricsBindAddress", cfgParams.MetricsBindAddress)

	kConfig, err := kubutils.KubernetesDefaultConfigCreate()
	if err != nil {
		// Without a config the manager cannot be built at all; continuing only moved
		// the failure a few lines down, to a message that does not name the cause.
		log.Error("unable to KubernetesDefaultConfigCreate", logger.Err(err))
		os.Exit(1)
	}
	log.Info("kubernetes config has been successfully created.")

	scheme := apiruntime.NewScheme()
	for _, f := range resourcesSchemeFuncs {
		err := f(scheme)
		if err != nil {
			log.Error("unable to add scheme to func", logger.Err(err))
			os.Exit(1)
		}
	}
	log.Info("successfully read scheme CR")

	cacheOpt := cache.Options{
		DefaultNamespaces: map[string]cache.Config{
			cfgParams.ControllerNamespace: {},
		},
	}

	managerOpts := manager.Options{
		Scheme: scheme,
		Cache:  cacheOpt,
		// The certificate validity gauges live in tls_metrics.go; without a listener
		// they would be updated and never read.
		Metrics:                 metricsserver.Options{BindAddress: cfgParams.MetricsBindAddress},
		HealthProbeBindAddress:  cfgParams.HealthProbeBindAddress,
		LeaderElection:          true,
		LeaderElectionNamespace: cfgParams.ControllerNamespace,
		LeaderElectionID:        config.ControllerName,
		Logger:                  log.GetLogger(),
	}

	mgr, err := manager.New(kConfig, managerOpts)
	if err != nil {
		log.Error("unable to manager.New", logger.Err(err))
		os.Exit(1)
	}
	log.Info("successfully created kubernetes manager")

	if _, err = controller.RunNFSStorageClassWatcherController(mgr, *cfgParams, baseLog.Named(controller.NFSStorageClassCtrlName)); err != nil {
		log.Error(fmt.Sprintf("unable to run %s", controller.NFSStorageClassCtrlName), logger.Err(err))
		os.Exit(1)
	}

	if _, err = controller.RunModuleConfigWatcherController(mgr, *cfgParams, baseLog.Named(controller.ModuleConfigCtrlName)); err != nil {
		log.Error(fmt.Sprintf("unable to run %s", controller.ModuleConfigCtrlName), logger.Err(err))
		os.Exit(1)
	}

	controller.RunNodeSelectorReconciler(ctx, mgr, *cfgParams, baseLog.Named(controller.NodeSelectorReconcilerName))

	// The validity of the RPC-with-TLS certificates, which nothing in this module
	// rotates: they come from the ModuleConfig and expire on a date the cluster has no
	// other way of knowing. See tls_metrics.go.
	//
	// Gated on the feature and not on the ConfigMap being there, so a cluster that turns
	// TLS on later needs no restart of this pod to start reporting; the pass itself
	// treats a missing ConfigMap as "no certificates configured".
	//
	// mgr.Add and not a bare goroutine: started directly it would run in every replica
	// and publish the same series from each of them, and it would run before mgr.Start,
	// hitting an API server the manager has not finished setting up.
	if commonfeature.TLSEnabled() {
		if err = mgr.Add(controller.RunTLSCertificateMetrics(mgr.GetAPIReader(), *cfgParams, baseLog.Named(controller.TLSCertificateMetricsName))); err != nil {
			log.Error(fmt.Sprintf("unable to add %s", controller.TLSCertificateMetricsName), logger.Err(err))
			os.Exit(1)
		}
	}

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error("unable to mgr.AddHealthzCheck", logger.Err(err))
		os.Exit(1)
	}
	log.Info("successfully AddHealthzCheck")

	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error("unable to mgr.AddReadyzCheck", logger.Err(err))
		os.Exit(1)
	}
	log.Info("successfully AddReadyzCheck")

	err = mgr.Start(ctx)
	if err != nil {
		log.Error("unable to mgr.Start", logger.Err(err))
		os.Exit(1)
	}
}
