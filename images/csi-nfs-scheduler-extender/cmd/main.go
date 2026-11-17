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
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/yaml"

	"github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/csi-nfs-scheduler-extender/pkg/kubutils"
	"github.com/deckhouse/csi-nfs/images/csi-nfs-scheduler-extender/pkg/scheduler"
	"github.com/deckhouse/sds-common-lib/logger"
	"github.com/deckhouse/sds-common-lib/monitoring"
	"github.com/deckhouse/sds-common-lib/monitoring/metricsauth"
)

const (
	defaultDivisor                = 1
	defaultListenAddr             = ":8000"
	defaultHealthProbeBindAddress = ":8081"
	defaultCacheSize              = 10
	defaultcertFile               = "/etc/csi-nfs-scheduler-extender/certs/tls.crt"
	defaultkeyFile                = "/etc/csi-nfs-scheduler-extender/certs/tls.key"
)

type Config struct {
	ListenAddr             string  `json:"listen"`
	DefaultDivisor         float64 `json:"default-divisor"`
	LogLevel               string  `json:"log-level"`
	CacheSize              int     `json:"cache-size"`
	HealthProbeBindAddress string  `json:"health-probe-bind-address"`
	CertFile               string  `json:"cert-file"`
	KeyFile                string  `json:"key-file"`
	PVCExpiredDurationSec  int     `json:"pvc-expired-duration-sec"`
}

var cfgFilePath string

var resourcesSchemeFuncs = []func(*runtime.Scheme) error{
	corev1.AddToScheme,
	storagev1.AddToScheme,
	v1alpha1.AddToScheme,
}

var config = &Config{
	ListenAddr:             defaultListenAddr,
	HealthProbeBindAddress: defaultHealthProbeBindAddress,
	LogLevel:               "2",
	CertFile:               defaultcertFile,
	KeyFile:                defaultkeyFile,
}

var rootCmd = &cobra.Command{
	Use:     "csi-nfs-scheduler-extender",
	Version: "development",
	Short:   "a scheduler-extender for csi-nfs",
	Long: `A scheduler-extender for csi-nfs.
The extender implements filter verbs.
The filter verb is "filter" and served at "/filter" via HTTP.
It filters out nodes that not selected by user's selectors.
`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		// to avoid printing usage information when error is returned
		cmd.SilenceUsage = true
		// to avoid printing errors (we log it closer to the place where it has happened)
		cmd.SilenceErrors = true
		return subMain(cmd.Context())
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFilePath, "config", "", "config file")
}

func main() {
	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		// we expect err to be logged already
		os.Exit(1)
	}
}

// defaultMetricsBindAddress keeps the endpoint on loopback when the chart says
// nothing: the fleet-wide fallback is the wildcard ":8080", which would answer
// unauthenticated on the pod IP.
const defaultMetricsBindAddress = "127.0.0.1:8080"

func subMain(ctx context.Context) error {
	if len(cfgFilePath) != 0 {
		b, err := os.ReadFile(cfgFilePath)
		if err != nil {
			print(err)
			return err
		}

		if err = yaml.Unmarshal(b, config); err != nil {
			print(err)
			return err
		}
	}

	log, err := logger.NewLogger(logger.Verbosity(config.LogLevel))
	if err != nil {
		print(fmt.Sprintf("[subMain] unable to initialize logger, err: %s", err))
		return err
	}
	subMainLog := log.Named("subMain")
	subMainLog.Info(fmt.Sprintf("logger has been initialized, log level: %s", config.LogLevel))
	ctrl.SetLogger(log.GetLogger())

	kConfig, err := kubutils.KubernetesDefaultConfigCreate()
	if err != nil {
		log.Error("unable to KubernetesDefaultConfigCreate", logger.Err(err))
		return err
	}
	subMainLog.Info("kubernetes config has been successfully created.")

	scheme := runtime.NewScheme()
	for _, f := range resourcesSchemeFuncs {
		if err := f(scheme); err != nil {
			log.Error("unable to add scheme to func", logger.Err(err))
			return err
		}
	}
	subMainLog.Info("successfully read scheme CR")

	managerOpts := manager.Options{
		Scheme: scheme,
		Cache:  cache.Options{DefaultTransform: cache.TransformStripManagedFields()},
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{&corev1.PersistentVolume{}},
			},
		},
		Logger: log.GetLogger(),
		// controller-runtime binds :8080 whenever this is empty, so this manager has
		// always served its reconcile and workqueue metrics with nothing declaring the
		// port. The chart publishes it over HTTPS and has every scrape authorized here.
		Metrics:                metricsauth.ApplyFromEnv(metricsserver.Options{BindAddress: monitoring.BindAddressFromEnvOr(defaultMetricsBindAddress)}),
		HealthProbeBindAddress: config.HealthProbeBindAddress,
		BaseContext:            func() context.Context { return ctx },
	}

	mgr, err := manager.New(kConfig, managerOpts)
	if err != nil {
		log.Error("unable to create manager for creating controllers", logger.Err(err))
		return err
	}

	httpHandler, err := scheduler.NewHandler(ctx, mgr.GetClient(), log)
	if err != nil {
		log.Error("unable to create http.Handler of the scheduler extender", logger.Err(err))
		return err
	}
	subMainLog.Info("scheduler handler initialized")

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error("unable to mgr.AddHealthzCheck", logger.Err(err))
		return err
	}
	subMainLog.Info("successfully AddHealthzCheck")

	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error("unable to mgr.AddReadyzCheck", logger.Err(err))
		return err
	}
	subMainLog.Info("successfully AddReadyzCheck")

	serv := &http.Server{
		Addr:         config.ListenAddr,
		Handler:      httpHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	subMainLog.Info("server was initialized")

	return runServer(ctx, serv, mgr, log)
}

func runServer(ctx context.Context, serv *http.Server, mgr manager.Manager, log logger.Logger) error {
	log = log.Named("runServer")
	ctx, stop := context.WithCancel(ctx)

	var wg sync.WaitGroup
	defer wg.Wait()
	defer stop() // stop() should be called before wg.Wait() to stop the goroutine correctly.
	wg.Add(1)

	go func() {
		defer wg.Done()
		<-ctx.Done()
		if err := serv.Shutdown(ctx); err != nil {
			log.Error("failed to shutdown gracefully", logger.Err(err))
		}
	}()

	go func() {
		log.Info("kube manager will start now")
		if err := mgr.Start(ctx); err != nil {
			log.Error("unable to mgr.Start", logger.Err(err))
			stop()
		}
	}()

	log.Info(fmt.Sprintf("starts serving on: %s", config.ListenAddr))

	if err := serv.ListenAndServeTLS(config.CertFile, config.KeyFile); !errors.Is(err, http.ErrServerClosed) {
		log.Error("unable to run the server", logger.Err(err))
		return err
	}

	return nil
}
