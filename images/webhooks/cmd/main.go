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
	"flag"
	"fmt"
	"net/http"
	"os"

	storagev1 "k8s.io/api/storage/v1"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/webhooks/handlers"
	commonfeature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
	d8commonapi "github.com/deckhouse/sds-common-lib/api/v1alpha1"
	"github.com/deckhouse/sds-common-lib/logger"
	"github.com/deckhouse/sds-common-lib/logger/kwhlogger"
)

type config struct {
	certFile string
	keyFile  string
}

func httpHandlerHealthz(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, "Ok.")
}

func initFlags() config {
	cfg := config{}

	fl := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fl.StringVar(&cfg.certFile, "tls-cert-file", "", "TLS certificate file")
	fl.StringVar(&cfg.keyFile, "tls-key-file", "", "TLS key file")

	_ = fl.Parse(os.Args[1:])
	return cfg
}

const (
	port           = ":8443"
	NSCValidatorID = "NSCValidator"
	SCValidatorID  = "SCValidator"
	MCValidatorID  = "MCValidator"
)

func main() {
	// LOG_LEVEL arrives as a number 0..4 from lib-helm, which is what the
	// controller container in this same pod already reads. This webhook used to
	// ignore it, so its level was whatever main hard-coded no matter what the
	// module setting said.
	log, err := logger.NewLoggerFromEnv(logger.InfoLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to create the logger: %s\n", err)
		os.Exit(1)
	}
	kwhLog := kwhlogger.New(log)
	// One logger per process: the handlers are plain functions with nowhere to pass
	// one through, so they read it from their own package.
	handlers.SetLogger(log)

	cfg := initFlags()

	scValidatingWebhookHandler, err := handlers.GetValidatingWebhookHandler(handlers.SCValidate, SCValidatorID, &storagev1.StorageClass{}, kwhLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating scValidatingWebhookHandler: %s", err)
		os.Exit(1)
	}

	nscValidatingWebhookHandler, err := handlers.GetValidatingWebhookHandler(handlers.NSCValidate, NSCValidatorID, &cn.NFSStorageClass{}, kwhLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating nscValidatingWebhookHandler: %s", err)
		os.Exit(1)
	}

	mcValidatingWebhookHandler, err := handlers.GetValidatingWebhookHandler(handlers.MCValidate, MCValidatorID, &d8commonapi.ModuleConfig{}, kwhLog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating mcValidatingWebhookHandler: %s", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.Handle("/sc-validate", scValidatingWebhookHandler)
	mux.Handle("/nsc-validate", nscValidatingWebhookHandler)
	mux.Handle("/mc-validate", mcValidatingWebhookHandler)
	mux.HandleFunc("/healthz", httpHandlerHealthz)

	log.Info("feature flags", "tlsEnabled", commonfeature.TLSEnabled())

	log.Info("listening", "address", port)
	err = http.ListenAndServeTLS(port, cfg.certFile, cfg.keyFile, mux)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error serving webhook: %s", err)
		os.Exit(1)
	}
}
