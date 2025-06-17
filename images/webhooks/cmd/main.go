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

	"github.com/sirupsen/logrus"
	kwhlogrus "github.com/slok/kubewebhook/v2/pkg/log/logrus"
	storagev1 "k8s.io/api/storage/v1"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	mc "github.com/deckhouse/deckhouse/deckhouse-controller/pkg/apis/deckhouse.io/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/webhooks/handlers"
	commonfeature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
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
	logrusLogEntry := logrus.NewEntry(logrus.New())
	logrusLogEntry.Logger.SetLevel(logrus.DebugLevel)
	logger := kwhlogrus.NewLogrus(logrusLogEntry)

	cfg := initFlags()

	scValidatingWebhookHandler, err := handlers.GetValidatingWebhookHandler(handlers.SCValidate, SCValidatorID, &storagev1.StorageClass{}, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating scValidatingWebhookHandler: %s", err)
		os.Exit(1)
	}

	nscValidatingWebhookHandler, err := handlers.GetValidatingWebhookHandler(handlers.NSCValidate, NSCValidatorID, &cn.NFSStorageClass{}, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating nscValidatingWebhookHandler: %s", err)
		os.Exit(1)
	}

	mcValidatingWebhookHandler, err := handlers.GetValidatingWebhookHandler(handlers.MCValidate, MCValidatorID, &mc.ModuleConfig{}, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating mcValidatingWebhookHandler: %s", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.Handle("/sc-validate", scValidatingWebhookHandler)
	mux.Handle("/nsc-validate", nscValidatingWebhookHandler)
	mux.Handle("/mc-validate", mcValidatingWebhookHandler)
	mux.HandleFunc("/healthz", httpHandlerHealthz)

	logger.Infof("Feature TLSEnabled:%t", commonfeature.TLSEnabled())

	logger.Infof("Listening on %s", port)
	err = http.ListenAndServeTLS(port, cfg.certFile, cfg.keyFile, mux)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error serving webhook: %s", err)
		os.Exit(1)
	}
}
