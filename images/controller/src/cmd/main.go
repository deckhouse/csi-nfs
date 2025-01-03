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

package main

import (
	"context"
	"d8-controller/pkg/config"
	"d8-controller/pkg/controller"
	"d8-controller/pkg/kubutils"
	"d8-controller/pkg/logger"
	"fmt"
	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"os"
	goruntime "runtime"

	"sigs.k8s.io/controller-runtime/pkg/cache"

	v1 "k8s.io/api/core/v1"
	sv1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	resourcesSchemeFuncs = []func(*apiruntime.Scheme) error{
		cn.AddToScheme,
		clientgoscheme.AddToScheme,
		extv1.AddToScheme,
		v1.AddToScheme,
		sv1.AddToScheme,
	}
)

func main() {
	ctx := context.Background()
	cfgParams := config.NewConfig()

	log, err := logger.NewLogger(cfgParams.Loglevel)
	if err != nil {
		fmt.Println(fmt.Sprintf("unable to create NewLogger, err: %v", err))
		os.Exit(1)
	}

	log.Info(fmt.Sprintf("[main] Go Version:%s ", goruntime.Version()))
	log.Info(fmt.Sprintf("[main] OS/Arch:Go OS/Arch:%s/%s ", goruntime.GOOS, goruntime.GOARCH))

	log.Info("[main] CfgParams has been successfully created")
	log.Info(fmt.Sprintf("[main] %s = %s", config.LogLevelEnvName, cfgParams.Loglevel))
	log.Info(fmt.Sprintf("[main] RequeueStorageClassInterval = %d", cfgParams.RequeueStorageClassInterval))

	kConfig, err := kubutils.KubernetesDefaultConfigCreate()
	if err != nil {
		log.Error(err, "[main] unable to KubernetesDefaultConfigCreate")
	}
	log.Info("[main] kubernetes config has been successfully created.")

	scheme := runtime.NewScheme()
	for _, f := range resourcesSchemeFuncs {
		err := f(scheme)
		if err != nil {
			log.Error(err, "[main] unable to add scheme to func")
			os.Exit(1)
		}
	}
	log.Info("[main] successfully read scheme CR")

	cacheOpt := cache.Options{
		DefaultNamespaces: map[string]cache.Config{
			cfgParams.ControllerNamespace: {},
		},
	}

	managerOpts := manager.Options{
		Scheme: scheme,
		Cache:  cacheOpt,
		//MetricsBindAddress: cfgParams.MetricsPort,
		HealthProbeBindAddress:  cfgParams.HealthProbeBindAddress,
		LeaderElection:          true,
		LeaderElectionNamespace: cfgParams.ControllerNamespace,
		LeaderElectionID:        config.ControllerName,
		Logger:                  log.GetLogger(),
	}

	mgr, err := manager.New(kConfig, managerOpts)
	if err != nil {
		log.Error(err, "[main] unable to manager.New")
		os.Exit(1)
	}
	log.Info("[main] successfully created kubernetes manager")

	if _, err = controller.RunNFSStorageClassWatcherController(mgr, *cfgParams, *log); err != nil {
		log.Error(err, fmt.Sprintf("[main] unable to run %s", controller.NFSStorageClassCtrlName))
		os.Exit(1)
	}

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "[main] unable to mgr.AddHealthzCheck")
		os.Exit(1)
	}
	log.Info("[main] successfully AddHealthzCheck")

	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "[main] unable to mgr.AddReadyzCheck")
		os.Exit(1)
	}
	log.Info("[main] successfully AddReadyzCheck")

	err = mgr.Start(ctx)
	if err != nil {
		log.Error(err, "[main] unable to mgr.Start")
		os.Exit(1)
	}
}
